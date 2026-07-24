package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	summaryprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/summary"
)

type summarySummarizer struct {
	modelFactory ModelFactory
	cfg          config.EinoConfig
}

func NewSummarySummarizer(factory ModelFactory, cfg config.EinoConfig) contextmanager.RollingSummarizer {
	return &summarySummarizer{modelFactory: factory, cfg: cfg}
}

// 压缩消息
func (s *summarySummarizer) Summarize(ctx context.Context, req contextmanager.SummarizeRequest) (contextmanager.SummarizeResult, error) {
	if s == nil || s.modelFactory == nil {
		return contextmanager.SummarizeResult{}, ErrModelUnavailable
	}
	chatModel, err := s.modelFactory.NewChatModel(ctx, s.cfg)
	if err != nil {
		return contextmanager.SummarizeResult{}, err
	}
	userPrompt, err := buildSummaryUserPrompt(req)
	if err != nil {
		return contextmanager.SummarizeResult{}, err
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(summaryprompt.SystemPrompt),
		schema.UserMessage(userPrompt),
	})
	if err != nil {
		return contextmanager.SummarizeResult{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return contextmanager.SummarizeResult{}, contextmanager.ErrInvalidSummaryOutput
	}
	result := contextmanager.SummarizeResult{RawOutput: response.Content}
	// 记录模型调用的token数量：输入tokens、输出tokens、总tokens
	if response.ResponseMeta != nil && response.ResponseMeta.Usage != nil {
		result.PromptTokens = response.ResponseMeta.Usage.PromptTokens
		result.CompletionTokens = response.ResponseMeta.Usage.CompletionTokens
		result.TotalTokens = response.ResponseMeta.Usage.TotalTokens
	}
	return result, nil
}

type summaryPromptPayload struct {
	Instruction    string                 `json:"instruction"`
	UserID         uint64                 `json:"user_id"`
	ConversationID string                 `json:"conversation_id"`
	Previous       *summaryPreviousPrompt `json:"previous_summary,omitempty"`
	Messages       []summaryMessagePrompt `json:"messages_to_compress"`
}

type summaryPreviousPrompt struct {
	Summary   string         `json:"summary"`
	KeyFacts  map[string]any `json:"key_facts"`
	OpenTasks []string       `json:"open_tasks"`
}

type summaryMessagePrompt struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// 生成压缩消息的user prompt
func buildSummaryUserPrompt(req contextmanager.SummarizeRequest) (string, error) {
	payload := summaryPromptPayload{
		Instruction:    "请基于 previous_summary 和 messages_to_compress 生成新的滚动摘要。user_id 与 conversation_id 仅用于内部分段，不得写入输出。",
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		Messages:       buildSummaryMessagePrompts(req.Messages),
	}
	if req.Previous != nil {
		payload.Previous = &summaryPreviousPrompt{
			Summary:   req.Previous.Summary,
			KeyFacts:  req.Previous.KeyFacts,
			OpenTasks: req.Previous.OpenTasks,
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// 组装未被压缩的消息
func buildSummaryMessagePrompts(messages []*aimessages.AiMessages) []summaryMessagePrompt {
	result := make([]summaryMessagePrompt, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		result = append(result, summaryMessagePrompt{
			ID:        message.Id,
			Role:      message.Role,
			Content:   redactSummarySensitiveContext(message.Content),
			CreatedAt: formatSummaryTime(message.CreatedAt),
		})
	}
	return result
}

func formatSummaryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

var (
	summarySensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*=\s*[^\s,，;；]+`)
	summarySensitiveColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*[:：]\s*[^\s,，;；]+`)
)

// redactSummarySensitiveContext 脱敏敏感上下文
func redactSummarySensitiveContext(content string) string {
	content = summarySensitiveAssignmentPattern.ReplaceAllString(content, "$1=[redacted]")
	return summarySensitiveColonPattern.ReplaceAllString(content, "$1:[redacted]")
}

var _ contextmanager.RollingSummarizer = (*summarySummarizer)(nil)
