package contextmanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	summaryTriggerMessageCount  = 30
	summaryCompressMessageCount = 10
	summaryRecentMessageCount   = 20
	summaryQueryMessageLimit    = summaryTriggerMessageCount
	maxSummaryRefreshRounds     = 3
	maxSummaryChars             = 4000
	maxOpenTasks                = 12
)

var (
	ErrSummaryManagerUnavailable = errors.New("summary manager unavailable")
	ErrInvalidSummaryRequest     = errors.New("invalid summary refresh request")
	ErrInvalidSummaryOutput      = errors.New("invalid summary output")
)

type SummaryMessagesStore interface {
	CountUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string) (int64, error)
	FindUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error)
	FindRecentUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error)
}

type SummaryPersistence interface {
	FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error)
	Save(ctx context.Context, userID uint64, conversationID string, summary *domain.ConversationSummary) error
}

type RollingSummarizer interface {
	Summarize(ctx context.Context, req SummarizeRequest) (SummarizeResult, error)
}

type SummaryManager struct {
	summaries  SummaryPersistence
	messages   SummaryMessagesStore
	summarizer RollingSummarizer
	budget     domain.TokenBudget
	estimator  domain.TokenEstimator
}

type SummaryOption func(*SummaryManager)

// WithSummaryTokenBudget 注入上下文 token 预算，用于摘要触发的 token 阈值判断。
func WithSummaryTokenBudget(budget domain.TokenBudget) SummaryOption {
	return func(m *SummaryManager) { m.budget = budget }
}

type SummaryRefreshRequest struct {
	UserID         uint64
	ConversationID string
}

type SummaryRefreshResult struct {
	Created              bool
	Summary              *domain.ConversationSummary
	RecentMessages       []*aimessages.AiMessages
	CompressedMessageIDs []string
}

type SummarizeRequest struct {
	UserID         uint64
	ConversationID string
	Previous       *domain.ConversationSummary
	Messages       []*aimessages.AiMessages
}

type SummarizeResult struct {
	RawOutput        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func NewSummaryManager(summaries SummaryPersistence, messages SummaryMessagesStore, summarizer RollingSummarizer, opts ...SummaryOption) *SummaryManager {
	m := &SummaryManager{
		summaries: summaries, messages: messages, summarizer: summarizer,
		budget: domain.TokenBudget{}.Normalized(), estimator: domain.HybridEstimator{},
	}
	for _, opt := range opts {
		opt(m)
	}
	m.budget = m.budget.Normalized()
	return m
}

// CompressIfNeeded 判断未压缩对话流是否超阈值，超则同步压缩最旧部分并推进水位。
// 返回本次压缩的消息 ID（nil 表示未发生压缩）。实现 memory.Compressor，供每次模型调用前的动态压缩调用。
func (m *SummaryManager) CompressIfNeeded(ctx context.Context, userID uint64, conversationID string) ([]string, error) {
	result, err := m.MaybeRefresh(ctx, SummaryRefreshRequest{UserID: userID, ConversationID: conversationID})
	if err != nil {
		return nil, err
	}
	return result.CompressedMessageIDs, nil
}

// MaybeRefresh 检查是否需要刷新摘要，如果需要则生成新的摘要并保存
func (m *SummaryManager) MaybeRefresh(ctx context.Context, req SummaryRefreshRequest) (SummaryRefreshResult, error) {
	if req.UserID == 0 || req.ConversationID == "" {
		return SummaryRefreshResult{}, ErrInvalidSummaryRequest
	}
	if m == nil || m.summaries == nil || m.messages == nil {
		return SummaryRefreshResult{}, ErrSummaryManagerUnavailable
	}
	// 查找最新的摘要
	previous, err := m.summaries.FindLatest(ctx, req.UserID, req.ConversationID)
	if err != nil {
		return SummaryRefreshResult{}, err
	}
	var afterCreatedAt time.Time
	var afterMessageID string
	if previous != nil {
		afterCreatedAt = previous.CoveredUntilCreatedAt
		afterMessageID = previous.CoveredUntilMessageID
	}
	recentMessages, err := m.messages.FindRecentUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID, summaryRecentMessageCount)
	if err != nil {
		return SummaryRefreshResult{}, err
	}
	result := SummaryRefreshResult{Summary: previous, RecentMessages: recentMessages}
	unsummarizedCount, err := m.messages.CountUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID)
	if err != nil {
		return SummaryRefreshResult{}, err
	}
	// 估算一批未压缩消息的 token（最多 summaryQueryMessageLimit 条），作为 token 触发判断依据。
	sample, err := m.messages.FindUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID, summaryQueryMessageLimit)
	if err != nil {
		return SummaryRefreshResult{}, err
	}
	sampleTokens := 0
	for _, msg := range sample {
		if msg != nil {
			sampleTokens += m.estimator.Tokens(msg.Content)
		}
	}
	// 触发条件：累计 token 达到阈值（主），或条数达到兜底阈值（防估算偏差导致 backlog 失控）。
	if sampleTokens < m.budget.SummaryTriggerTokens && unsummarizedCount < summaryTriggerMessageCount {
		return result, nil
	}
	logx.Infof("会话 %s 用户 %d 有 %d 条未压缩消息，触发摘要刷新", req.ConversationID, req.UserID, unsummarizedCount)

	// 从最旧往新压缩：只要未压缩条数仍超过保留窗口（summaryRecentMessageCount）就继续，
	// 每轮最多压缩 summaryCompressMessageCount 条，最多 maxSummaryRefreshRounds 轮。
	// 这样既能响应 token 触发的长消息会话（条数不足 30 也压），也保持条数兜底的记忆维护节奏。
	for round := 0; round < maxSummaryRefreshRounds; round++ {
		if unsummarizedCount <= summaryRecentMessageCount {
			break
		}
		if m.summarizer == nil {
			return result, ErrSummaryManagerUnavailable
		}
		unsummarized, err := m.messages.FindUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID, summaryQueryMessageLimit)
		if err != nil {
			return result, err
		}
		if len(unsummarized) <= summaryRecentMessageCount {
			break
		}
		toCompressCount := summaryCompressMessageCount
		if remain := len(unsummarized) - summaryRecentMessageCount; remain < toCompressCount {
			toCompressCount = remain
		}
		toCompress := unsummarized[:toCompressCount]
		summaryResult, err := m.summarizer.Summarize(ctx, SummarizeRequest{
			UserID:         req.UserID,
			ConversationID: req.ConversationID,
			Previous:       previous,
			Messages:       toCompress,
		})

		if err != nil {
			return result, err
		}
		next, err := parseSummaryOutput(summaryResult.RawOutput)
		if err != nil {
			return result, err
		}
		watermark := toCompress[len(toCompress)-1]
		next.CoveredUntilMessageID = watermark.MsgId
		next.CoveredUntilCreatedAt = watermark.CreatedAt
		next.TokenCount = summaryTokenCount(summaryResult, next)
		if err := m.summaries.Save(ctx, req.UserID, req.ConversationID, next); err != nil {
			return result, err
		}
		result.Created = true
		result.Summary = next
		for _, message := range toCompress {
			if message != nil && strings.TrimSpace(message.MsgId) != "" {
				result.CompressedMessageIDs = append(result.CompressedMessageIDs, message.MsgId)
			}
		}
		previous = next
		afterCreatedAt = next.CoveredUntilCreatedAt
		afterMessageID = next.CoveredUntilMessageID
		unsummarizedCount -= int64(toCompressCount)
	}

	recentMessages, err = m.messages.FindRecentUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID, summaryRecentMessageCount)
	if err != nil {
		return result, err
	}
	result.RecentMessages = recentMessages
	return result, nil
}

// parseSummaryOutput 解析摘要模型的输出
func parseSummaryOutput(raw string) (*domain.ConversationSummary, error) {
	var output struct {
		Summary   string         `json:"summary"`
		KeyFacts  map[string]any `json:"key_facts"`
		OpenTasks []string       `json:"open_tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, ErrInvalidSummaryOutput
	}
	if output.Summary == "" || len([]rune(output.Summary)) > maxSummaryChars {
		return nil, ErrInvalidSummaryOutput
	}
	if output.KeyFacts == nil {
		output.KeyFacts = map[string]any{}
	}
	if len(output.OpenTasks) > maxOpenTasks {
		return nil, ErrInvalidSummaryOutput
	}
	return &domain.ConversationSummary{
		Summary:   output.Summary,
		KeyFacts:  output.KeyFacts,
		OpenTasks: compactStrings(output.OpenTasks),
	}, nil
}

func tailMessages(rows []*aimessages.AiMessages, limit int) []*aimessages.AiMessages {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[len(rows)-limit:]
}

func summaryTokenCount(result SummarizeResult, summary *domain.ConversationSummary) int {
	if result.CompletionTokens > 0 {
		return result.CompletionTokens
	}
	return estimateSummaryContentTokens(summary)
}

// estimateSummaryContentTokens 在模型没有返回 usage 时，近似估算摘要内容的 token 数量
func estimateSummaryContentTokens(summary *domain.ConversationSummary) int {
	if summary == nil {
		return 0
	}
	raw, _ := json.Marshal(summary)
	return domain.EstimateTokens(string(raw))
}

// compactStrings 过滤掉空字符串
func compactStrings(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func newSummaryID() string {
	return "summary_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}
