package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const (
	// defaultHistoryQueryLimit 是最近消息的查询素材上限，实际保留多少由 token 预算决定。
	defaultHistoryQueryLimit        = 200
	defaultRecentToolRefLimit       = 20
	defaultRelevantMemoryEventLimit = 5
)

type MessageStore interface {
	FindRecent(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error)
}

type SummaryStore interface {
	FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error)
}

type ToolContextStore interface {
	FindLatestToolResult(ctx context.Context, userID uint64, conversationID string) (*aitoolcalls.AiToolCalls, error)
	FindRecentToolCall(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aitoolcalls.AiToolCalls, error)
}

type TaskStateStore interface {
	FindActive(ctx context.Context, userID uint64, conversationID, runID string) (*domain.TaskState, error)
}

type UserProfileStore interface {
	LoadActive(ctx context.Context, userID uint64) (*domain.UserProfile, error)
}

type MemoryEventStore interface {
	ListRecentUserMemoryEvents(ctx context.Context, userID uint64, limit int) ([]Event, error)
	SearchUserMemoryEvents(ctx context.Context, query UserMemoryEventQuery) ([]Event, error)
}

type CustomerServiceProviderConfig struct {
	Messages     MessageStore
	Summaries    SummaryStore
	Tools        ToolContextStore
	TaskStates   TaskStateStore
	Profiles     UserProfileStore
	Events       MemoryEventStore
	Compressor   Compressor
	OnCompressed CompressCallback
	Budget       domain.TokenBudget
	Now          func() time.Time
}

// CustomerServiceProvider 是 MemoryProvider 的薄门面：上下文组装委托给
// ContextBuilder（内部再调用 MemoryBuilder 取数），自身保留长期事件搜索能力供
// capability 工具使用。
type CustomerServiceProvider struct {
	context *ContextBuilder
	events  MemoryEventStore
}

func NewCustomerServiceProvider(cfg CustomerServiceProviderConfig) *CustomerServiceProvider {
	memory := NewMemoryBuilder(cfg.Messages, cfg.Summaries, cfg.Profiles, cfg.Events)
	context := NewContextBuilder(ContextBuilderConfig{
		Memory:       memory,
		Compressor:   cfg.Compressor,
		Tools:        cfg.Tools,
		Tasks:        cfg.TaskStates,
		Budget:       cfg.Budget,
		OnCompressed: cfg.OnCompressed,
		Now:          cfg.Now,
	})
	return &CustomerServiceProvider{context: context, events: cfg.Events}
}

func (p *CustomerServiceProvider) Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResult, error) {
	if req == nil || req.UserID == 0 || req.ConversationID == "" || p == nil || p.context == nil {
		return &RetrieveResult{Metadata: map[string]any{}}, nil
	}
	result := &RetrieveResult{Metadata: map[string]any{}}
	// 稳定前缀（用户画像 + 长期事件）：同一 turn 内 SkipStable 跳过（已在 ctx 缓存）。
	if !req.SkipStable {
		result.SystemMessages = p.context.PrepareStable(ctx, req)
	}
	// 易变后缀（摘要 + 历史 + 任务 + 工具 + 时间 + RAG）：每次模型调用前刷新。
	volatile, err := p.context.RefreshVolatile(ctx, req)
	if err != nil {
		return result, err
	}
	result.HistoryMessages = volatile.HistoryMessages
	result.ContextMessages = volatile.ContextMessages
	result.Metadata = volatile.Metadata
	return result, nil
}

func (p *CustomerServiceProvider) Memorize(ctx context.Context, req *MemorizeRequest) error {
	return nil
}

func (p *CustomerServiceProvider) Close() error {
	return nil
}

func (p *CustomerServiceProvider) SearchUserMemoryEvents(ctx context.Context, query UserMemoryEventQuery) ([]Event, error) {
	if p == nil || p.events == nil || query.UserID == 0 {
		return nil, nil
	}
	return p.events.SearchUserMemoryEvents(ctx, query)
}

// historyMessages 合并过滤排序、token 滑动窗口与脱敏，保留供测试与向后兼容。
func historyMessages(rows []*aimessages.AiMessages, currentMessageID string, summary *domain.ConversationSummary, budget domain.TokenBudget, estimator domain.TokenEstimator) []domain.ContextMessage {
	return windowAndRedactHistory(filterAndSortHistory(rows, currentMessageID, summary), budget, estimator)
}

// coveredBySummary 判断消息是否已被摘要覆盖。
func coveredBySummary(message *aimessages.AiMessages, summary *domain.ConversationSummary) bool {
	if message == nil || summary == nil || summary.CoveredUntilCreatedAt.IsZero() {
		return false
	}
	if message.CreatedAt.Before(summary.CoveredUntilCreatedAt) {
		return true
	}
	return message.CreatedAt.Equal(summary.CoveredUntilCreatedAt) && message.MsgId <= summary.CoveredUntilMessageID
}

func jsonBlock(label string, value any) (domain.ContextMessage, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return domain.ContextMessage{}, false
	}
	return domain.ContextMessage{Role: domain.ContextRoleUser, Content: formatBlock(label, string(raw))}, true
}

func formatBlock(label, content string) string {
	return fmt.Sprintf("<%s>\n%s\n</%s>", label, content, label)
}

var (
	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|auth)\b\s*=\s*[^\s,，;；]+`)
	sensitiveColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|auth)\b\s*[:：]\s*[^\s,，;；]+`)
)

func redactSensitiveContext(content string) string {
	content = sensitiveAssignmentPattern.ReplaceAllString(content, "$1=[redacted]")
	return sensitiveColonPattern.ReplaceAllString(content, "$1:[redacted]")
}

// eventRecallQuery 从当前 turn 的最近 user 消息提取事件检索 query。
func eventRecallQuery(req *RetrieveRequest) string {
	if req == nil {
		return ""
	}
	parts := make([]string, 0, len(req.Messages))
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := req.Messages[i]
		if message.Role != domain.ContextRoleUser {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		parts = append(parts, content)
		if len(parts) >= 2 {
			break
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func recallKeywords(text string, agentName string) []string {
	keywords := compactKeywords([]string{agentName})
	seen := make(map[string]struct{}, len(keywords)+8)
	for _, item := range keywords {
		seen[strings.ToLower(item)] = struct{}{}
	}
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return r <= ' ' || strings.ContainsRune(",.?!;:，。？！；：、()[]{}<>\"'`", r)
	}) {
		token = strings.TrimSpace(token)
		if len([]rune(token)) < 2 || len([]rune(token)) > 32 {
			continue
		}
		key := strings.ToLower(token)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keywords = append(keywords, token)
		if len(keywords) >= 6 {
			break
		}
	}
	return keywords
}

func dedupeEvents(primary []Event, already []Event) []Event {
	if len(primary) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(primary)+len(already))
	for _, event := range already {
		seen[eventKey(event)] = struct{}{}
	}
	result := make([]Event, 0, len(primary))
	for _, event := range primary {
		key := eventKey(event)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, event)
	}
	return result
}

func eventKey(event Event) string {
	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		return ""
	}
	return fmt.Sprintf("%d|%s|%s|%s", event.UserID, event.Type, event.EventDate, summary)
}
