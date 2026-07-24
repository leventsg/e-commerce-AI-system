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
}

type SummaryRefreshRequest struct {
	UserID         uint64
	ConversationID string
}

type SummaryRefreshResult struct {
	Created        bool
	Summary        *domain.ConversationSummary
	RecentMessages []*aimessages.AiMessages
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

func NewSummaryManager(summaries SummaryPersistence, messages SummaryMessagesStore, summarizer RollingSummarizer) *SummaryManager {
	return &SummaryManager{summaries: summaries, messages: messages, summarizer: summarizer}
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
	if unsummarizedCount < summaryTriggerMessageCount {
		return result, nil
	}

	// 单次最多压缩 3 轮，每次压缩 10 条消息，最多压缩 30 条消息
	for round := 0; round < maxSummaryRefreshRounds && unsummarizedCount >= summaryTriggerMessageCount; round++ {
		if m.summarizer == nil {
			return result, ErrSummaryManagerUnavailable
		}
		unsummarized, err := m.messages.FindUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID, summaryQueryMessageLimit)
		if err != nil {
			return result, err
		}
		// 如果未压缩的消息数量不足，则不进行压缩
		if len(unsummarized) < summaryTriggerMessageCount {
			recentMessages, recentErr := m.messages.FindRecentUnsummarized(ctx, req.UserID, req.ConversationID, afterCreatedAt, afterMessageID, summaryRecentMessageCount)
			if recentErr != nil {
				return result, recentErr
			}
			result.RecentMessages = recentMessages
			return result, nil
		}
		toCompress := unsummarized[:summaryCompressMessageCount]
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
		next.CoveredUntilMessageID = watermark.Id
		next.CoveredUntilCreatedAt = watermark.CreatedAt
		next.TokenCount = summaryTokenCount(summaryResult, next)
		if err := m.summaries.Save(ctx, req.UserID, req.ConversationID, next); err != nil {
			return result, err
		}
		result.Created = true
		result.Summary = next
		previous = next
		afterCreatedAt = next.CoveredUntilCreatedAt
		afterMessageID = next.CoveredUntilMessageID
		unsummarizedCount -= summaryCompressMessageCount
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
	// 一个token大约是一到两个中文字符，向上取整
	return (len([]rune(string(raw))) + 3) / 2
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
