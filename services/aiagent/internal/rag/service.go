package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	ragprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/rag"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type Service struct {
	cfg        config.RAGConfig
	classifier *ragClassifier
	client     *RagentClient
	cache      *VectorCache
	messages   ConversationContextReader
	summaries  ConversationSummaryReader
}

func NewService(cfg config.RAGConfig, modelFactory eino.ModelFactory, messages ConversationContextReader, summaries ConversationSummaryReader, redisClient *redis.Client, mainModelConfig config.EinoConfig) *Service {
	return &Service{
		cfg:        cfg,
		classifier: newRagClassifier(modelFactory, mainModelConfig),
		client:     NewRagentClient(cfg),
		cache:      NewVectorCache(redisClient, cfg),
		messages:   messages,
		summaries:  summaries,
	}
}

func (s *Service) Config() config.RAGConfig {
	return s.cfg
}

func (s *Service) Prepare(ctx context.Context, req PrepareRequest) (*PrepareResult, error) {
	if s == nil || !s.cfg.Enabled || s.client == nil || s.classifier == nil {
		return nil, nil
	}
	if req.UserID == 0 || req.ConversationID == "" ||
		req.Content.Query == "" || req.Content.EmbeddingModel == "" || s.cfg.APIKey == "" {
		return nil, nil
	}

	classifyInput, err := s.buildClassifierInput(ctx, req)
	if err != nil {
		logx.Errorw("build rag classifier input failed", logx.Field("conversation_id", req.ConversationID), logx.Field("user_id", req.UserID), logx.Field("err", err))
		return nil, nil
	}
	decision, err := s.classifier.Classify(ctx, classifyInput)
	if err != nil {
		logx.Errorw("rag classify failed, skip rag", logx.Field("conversation_id", req.ConversationID), logx.Field("user_id", req.UserID), logx.Field("err", err))
		return nil, nil
	}
	if !decision.NeedRAG || decision.Confidence < s.cfg.ConfidenceThreshold {
		return nil, nil
	}

	query := req.Content.Query
	embeddingModel := req.Content.EmbeddingModel
	topK := s.cfg.TopK
	if topK <= 0 {
		topK = 5
	}

	queryVector, err := s.client.Embed(ctx, query, embeddingModel)
	if err != nil {
		logx.Errorw("rag embed failed, skip rag", logx.Field("conversation_id", req.ConversationID), logx.Field("user_id", req.UserID), logx.Field("err", err))
		return nil, nil
	}
	if len(queryVector) == 0 {
		logx.Errorw("rag embed returned empty vector, skip rag", logx.Field("conversation_id", req.ConversationID), logx.Field("user_id", req.UserID))
		return nil, nil
	}
	if s.cache != nil {
		if cached, ok := s.cache.Get(ctx, queryVector); ok {
			var cachedSources []domain.AgentSource
			if err := json.Unmarshal(cached, &cachedSources); err == nil && len(cachedSources) > 0 {
				logx.Infow("rag vector cache hit", logx.Field("conversation_id", req.ConversationID), logx.Field("user_id", req.UserID), logx.Field("query", query), logx.Field("source_count", len(cachedSources)))
				return s.buildResult(cachedSources), nil
			}
		}
	}

	outcome, err := s.client.Retrieve(ctx, query, topK)
	if err != nil {
		logx.Errorw("rag retrieve failed, skip rag", logx.Field("conversation_id", req.ConversationID), logx.Field("user_id", req.UserID), logx.Field("err", err))
		return nil, nil
	}
	if outcome == nil || len(outcome.Chunks) == 0 {
		return nil, nil
	}

	sources := buildSources(outcome.Chunks, s.cfg)
	if len(sources) == 0 {
		return nil, nil
	}
	cacheVector := outcome.QueryEmbedding
	if len(cacheVector) == 0 {
		cacheVector = queryVector
	}
	if s.cache != nil && len(cacheVector) > 0 {
		raw, _ := json.Marshal(sources)
		if err := s.cache.Set(ctx, query, cacheVector, raw); err != nil {
			logx.Errorw("rag vector cache write failed", logx.Field("conversation_id", req.ConversationID), logx.Field("err", err))
		}
	}
	return s.buildResult(sources), nil
}

// 组装RAG分类器输入文本
func (s *Service) buildClassifierInput(ctx context.Context, req PrepareRequest) (string, error) {
	summaryText := ""
	if s.summaries != nil {
		if summary, err := s.summaries.FindLatest(ctx, req.UserID, req.ConversationID); err == nil && summary != nil {
			summaryText = summary.Summary
		}
	}

	recent := make([]string, 0, s.recentMessageLimit())
	if s.messages != nil {
		if rows, err := s.messages.FindRecent(ctx, req.UserID, req.ConversationID, s.recentMessageLimit()+1); err == nil {
			recent = recentConversationText(rows, req.CurrentMessageID, s.recentMessageLimit())
		}
	}
	if summaryText == "" {
		summaryText = "（无摘要）"
	}
	recentText := strings.Join(recent, "\n")
	if strings.TrimSpace(recentText) == "" {
		recentText = "（暂无最近对话）"
	}
	return fmt.Sprintf(ragprompt.ContextFormat, summaryText, recentText, strings.TrimSpace(req.Content.Query)), nil
}

func (s *Service) recentMessageLimit() int {
	if s.cfg.RecentMessages <= 0 {
		return 6
	}
	return s.cfg.RecentMessages
}

func (s *Service) buildResult(sources []domain.AgentSource) *PrepareResult {
	return &PrepareResult{
		ContextMessages: []domain.ContextMessage{{
			Role:    domain.ContextRoleSystem,
			Content: formatRAGContext(sources),
		}},
		Sources: sources,
	}
}

func recentConversationText(rows []*aimessages.AiMessages, currentMessageID string, limit int) []string {
	result := make([]string, 0, limit)
	for _, row := range rows {
		if row == nil || row.MsgId == currentMessageID || strings.TrimSpace(row.Content) == "" {
			continue
		}
		if row.Role != domain.ContextRoleUser && row.Role != domain.ContextRoleAssistant {
			continue
		}
		switch row.Role {
		case domain.ContextRoleUser:
			result = append(result, "用户："+row.Content)
		default:
			result = append(result, "客服："+row.Content)
		}
	}
	if len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result
}

func buildSources(chunks []retrievedChunk, cfg config.RAGConfig) []domain.AgentSource {
	previewChars := cfg.SourcePreviewChars
	if previewChars <= 0 {
		previewChars = 200
	}
	previewBase := strings.TrimRight(strings.TrimSpace(cfg.PreviewBaseURL), "/")
	byDocument := make(map[string]*domain.AgentSource)
	var order []string
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.DocumentID) == "" || strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		docID := chunk.DocumentID
		source, ok := byDocument[docID]
		if !ok {
			title := strings.TrimSpace(chunk.DocumentTitle)
			if title == "" {
				title = docID
			}
			source = &domain.AgentSource{
				DocumentID:  docID,
				Title:       title,
				DocumentURL: previewBase + "/preview/doc/" + docID,
			}
			byDocument[docID] = source
			order = append(order, docID)
		}
		source.Chunks = append(source.Chunks, domain.AgentSourceChunk{
			ChunkID: chunk.ChunkID,
			Content: truncateRune(chunk.Content, previewChars),
			Score:   chunk.Score,
		})
	}
	result := make([]domain.AgentSource, 0, len(order))
	for _, docID := range order {
		result = append(result, *byDocument[docID])
	}
	return result
}

func formatRAGContext(sources []domain.AgentSource) string {
	var builder strings.Builder
	builder.WriteString("【知识库参考】\n")
	for _, source := range sources {
		builder.WriteString("文档：")
		builder.WriteString(source.Title)
		builder.WriteString("\n")
		for _, chunk := range source.Chunks {
			builder.WriteString("- ")
			builder.WriteString(chunk.Content)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func truncateRune(value string, max int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}
