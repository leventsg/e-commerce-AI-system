package contextmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	agentprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/agent"
)

const (
	// recentMessageQueryLimit 是最近消息的查询上限（预算滑动的素材上限），
	// 实际保留多少条由 token 预算（RecentWindowTokens）决定，而非固定条数。
	recentMessageQueryLimit = 200
	recentToolRefLimit      = 20
)

var (
	ErrContextManagerUnavailable = errors.New("context manager unavailable")
	ErrInvalidContextRequest     = errors.New("invalid context request")
)

type Manager interface {
	Build(ctx context.Context, req domain.BuildContextRequest) (*domain.BuildContextResult, error)
}

type SummaryStore interface {
	FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error)
}

type TaskStateStore interface {
	FindActive(ctx context.Context, userID uint64, conversationID, runID string) (*domain.TaskState, error)
}

type UserProfileStore interface {
	LoadActive(ctx context.Context, userID uint64) (*domain.UserProfile, error)
}

type ToolContextStore interface {
	FindLatestToolResult(ctx context.Context, userID uint64, conversationID string) (*aitoolcalls.AiToolCalls, error)
	FindRecentToolCall(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aitoolcalls.AiToolCalls, error)
}

type manager struct {
	messages    MessageStore
	tools       ToolContextStore
	summaries   SummaryStore // 会话级记忆
	taskStates  TaskStateStore
	userProfile UserProfileStore
	budget      domain.TokenBudget
	estimator   domain.TokenEstimator
}

type Option func(*manager)

func WithSummaryStore(store SummaryStore) Option {
	return func(m *manager) { m.summaries = store }
}

func WithTaskStateStore(store TaskStateStore) Option {
	return func(m *manager) { m.taskStates = store }
}

func WithUserProfileStore(store UserProfileStore) Option {
	return func(m *manager) { m.userProfile = store }
}

// WithTokenBudget 注入上下文 token 预算。未调用时使用 Normalized 默认值。
func WithTokenBudget(budget domain.TokenBudget) Option {
	return func(m *manager) { m.budget = budget }
}

func NewManager(messages MessageStore, tools ToolContextStore, opts ...Option) Manager {
	m := &manager{
		messages:  messages,
		tools:     tools,
		budget:    domain.TokenBudget{}.Normalized(),
		estimator: domain.HybridEstimator{},
	}
	for _, opt := range opts {
		opt(m)
	}
	m.budget = m.budget.Normalized()
	return m
}

func (m *manager) Build(ctx context.Context, req domain.BuildContextRequest) (*domain.BuildContextResult, error) {
	// 验证请求参数
	if req.UserID == 0 || req.ConversationID == "" || req.CurrentInput == "" ||
		req.Mode != domain.AgentContextMode {
		return nil, ErrInvalidContextRequest
	}
	if m == nil || m.messages == nil {
		return nil, ErrContextManagerUnavailable
	}

	// 查询最近的未被压缩的消息记录（素材上限，实际保留多少由 token 预算决定）
	rows, err := m.messages.FindRecent(ctx, req.UserID, req.ConversationID, recentMessageQueryLimit)
	if err != nil {
		return nil, fmt.Errorf("load recent context messages: %w", err)
	}
	var summary *domain.ConversationSummary
	if req.Mode == domain.AgentContextMode && m.summaries != nil {
		// 获取最新的被压缩的长期记忆
		summary, _ = m.summaries.FindLatest(ctx, req.UserID, req.ConversationID)
	}
	recent, spilled := m.selectRecentByBudget(rows, req.CurrentMessageID, summary)
	result := &domain.BuildContextResult{
		Messages: make([]domain.ContextMessage, 0, len(recent)+8),
	}

	result.Messages = append(result.Messages, domain.ContextMessage{Role: domain.ContextRoleSystem, Content: agentprompt.SystemPrompt})
	m.appendSummary(summary, result)

	for _, row := range recent {
		result.Messages = append(result.Messages, domain.ContextMessage{Role: row.Role, Content: redactSensitiveContext(row.Content)})
	}
	if len(recent) > 0 {
		result.RecentMessageStartID = recent[0].MsgId
		result.RecentMessageEndID = recent[len(recent)-1].MsgId
	}

	m.appendToolContext(ctx, req, result)
	m.appendTaskState(ctx, req, result)
	m.appendUserProfile(ctx, req, result)

	result.Messages = append(result.Messages, domain.ContextMessage{Role: domain.ContextRoleUser, Content: req.CurrentInput})

	// 预算观测：估算用量与剩余预算，供 Langfuse/日志调参。
	result.EstimatedInputTokens = domain.EstimateMessagesTokens(result.Messages)
	result.BudgetTotalTokens = m.budget.EffectiveTotalTokens()
	result.BudgetUsedTokens = result.EstimatedInputTokens
	result.BudgetRemainingTokens = result.BudgetTotalTokens - result.BudgetUsedTokens
	if result.BudgetRemainingTokens < 0 {
		result.BudgetRemainingTokens = 0
	}
	result.SpilledMessageCount = len(spilled)
	return result, nil
}

var (
	sensitiveContextAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|auth)\b\s*=\s*[^\s,，;；]+`)
	sensitiveContextColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|auth)\b\s*[:：]\s*[^\s,，;；]+`)
)

func redactSensitiveContext(content string) string {
	content = sensitiveContextAssignmentPattern.ReplaceAllString(content, "$1=[redacted]")
	return sensitiveContextColonPattern.ReplaceAllString(content, "$1:[redacted]")
}

func (m *manager) appendSummary(summary *domain.ConversationSummary, result *domain.BuildContextResult) {
	if summary == nil {
		return
	}
	if message, ok := structuredContextMessage("conversation_summary", summary); ok {
		message.Content = m.clampContent(message.Content, m.budget.SummaryMaxTokens)
		result.Messages = append(result.Messages, message)
		result.SummaryCoveredMessageID = summary.CoveredUntilMessageID
		result.SummaryCoveredUntilCreatedAt = summary.CoveredUntilCreatedAt
	}
}

func (m *manager) appendToolContext(ctx context.Context, req domain.BuildContextRequest, result *domain.BuildContextResult) {
	if m.tools == nil {
		return
	}
	latest, err := m.tools.FindLatestToolResult(ctx, req.UserID, req.ConversationID)
	if err == nil && latest != nil {
		if message, ok := structuredContextMessage("latest_tool_result", latest); ok {
			message.Content = m.clampContent(message.Content, m.budget.LatestToolResultMaxTokens)
			result.Messages = append(result.Messages, message)
			result.LatestToolCallID = latest.ToolCallId
		}
	}

	recent, err := m.tools.FindRecentToolCall(ctx, req.UserID, req.ConversationID, recentToolRefLimit)
	if err != nil {
		return
	}
	historical := make([]*aitoolcalls.AiToolCalls, 0, len(recent))
	for _, call := range recent {
		if call == nil || call.ToolCallId == result.LatestToolCallID {
			continue
		}
		historical = append(historical, call)
	}
	if len(historical) == 0 {
		return
	}
	if message, ok := structuredContextMessage("recent_tool_calls", historical); ok {
		message.Content = m.clampContent(message.Content, m.budget.ToolHistoryMaxTokens)
		result.Messages = append(result.Messages, message)
		result.RecentToolCallCount = len(historical)
	}
}

func (m *manager) appendTaskState(ctx context.Context, req domain.BuildContextRequest, result *domain.BuildContextResult) {
	if m.taskStates == nil {
		return
	}
	state, err := m.taskStates.FindActive(ctx, req.UserID, req.ConversationID, req.RunID)
	if err != nil || state == nil {
		return
	}
	if message, ok := structuredContextMessage("task_state", state); ok {
		message.Content = m.clampContent(message.Content, m.budget.TaskStateMaxTokens)
		result.Messages = append(result.Messages, message)
	}
}

// appendUserProfile 将用户画像作为结构化消息附加到上下文中
func (m *manager) appendUserProfile(ctx context.Context, req domain.BuildContextRequest, result *domain.BuildContextResult) {
	if m.userProfile == nil {
		return
	}
	profile, err := m.userProfile.LoadActive(ctx, req.UserID)
	if err != nil || profile == nil {
		return
	}
	if message, ok := structuredContextMessage("user_profile", profile.ProfileJSON); ok {
		message.Content = m.clampContent(message.Content, m.budget.UserProfileMaxTokens)
		result.Messages = append(result.Messages, message)
	}
}

// clampContent 将内容截断到 maxTokens 以内，用于各非消息层的 cap。
func (m *manager) clampContent(content string, maxTokens int) string {
	if maxTokens <= 0 {
		return content
	}
	return domain.TruncateToTokens(content, maxTokens, m.estimator)
}

// selectRecentByBudget 从最新消息往回按 token 累计，形成滑动窗口。
// kept 按时间升序返回预算窗口内的消息；spilled 是更早、超出预算、待摘要覆盖的消息。
// 保底：即使预算耗尽也至少保留 MinRecentMessages 条，避免窗口为空。
func (m *manager) selectRecentByBudget(rows []*aimessages.AiMessages, currentMessageID string, summary *domain.ConversationSummary) (kept, spilled []*aimessages.AiMessages) {
	filtered := make([]*aimessages.AiMessages, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.MsgId == currentMessageID || row.Content == "" {
			continue
		}
		if row.Role != domain.ContextRoleUser && row.Role != domain.ContextRoleAssistant {
			continue
		}
		if coveredBySummary(row, summary) {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].MsgId < filtered[j].MsgId
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})

	remaining := m.budget.RecentWindowTokens
	minRecent := m.budget.MinRecentMessages
	kept = make([]*aimessages.AiMessages, 0, len(filtered))
	for i := len(filtered) - 1; i >= 0; i-- {
		cost := m.estimator.Tokens(filtered[i].Content)
		if len(kept) >= minRecent && remaining < cost {
			spilled = filtered[:i+1]
			break
		}
		kept = append([]*aimessages.AiMessages{filtered[i]}, kept...)
		remaining -= cost
	}
	return kept, spilled
}

func coveredBySummary(message *aimessages.AiMessages, summary *domain.ConversationSummary) bool {
	if message == nil || summary == nil || summary.CoveredUntilCreatedAt.IsZero() {
		return false
	}
	if message.CreatedAt.Before(summary.CoveredUntilCreatedAt) {
		return true
	}
	return message.CreatedAt.Equal(summary.CoveredUntilCreatedAt) && message.MsgId <= summary.CoveredUntilMessageID
}

func structuredContextMessage(label string, value any) (domain.ContextMessage, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return domain.ContextMessage{}, false
	}
	return domain.ContextMessage{
		Role: domain.ContextRoleAssistant, Content: "[" + label + "]\n" + string(raw),
	}, true
}
