package contextmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	agentprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/agent"
)

const (
	recentMessageLimit      = 20
	recentMessageQueryLimit = recentMessageLimit + 1
	recentToolRefLimit      = 20
	activeMemoryLimit       = 12
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

type MemoryStore interface {
	ListActive(ctx context.Context, userID uint64, limit int) ([]domain.UserMemory, error)
}

type TaskStateStore interface {
	FindActive(ctx context.Context, userID uint64, conversationID, runID string) (*domain.TaskState, error)
}

type UserProfileStore interface {
	LoadActive(ctx context.Context, userID uint64) (*domain.UserProfile, error)
}

type ToolContextStore interface {
	FindLatestResult(ctx context.Context, userID uint64, conversationID string) (*domain.ToolResultEnvelope, error)
	FindRecentRefs(ctx context.Context, userID uint64, conversationID string, limit int) ([]domain.ToolCallRef, error)
}

type manager struct {
	messages    MessageStore
	tools       ToolContextStore
	summaries   SummaryStore // 会话级记忆
	memories    MemoryStore  // 用户级记忆
	taskStates  TaskStateStore
	userProfile UserProfileStore
}

type Option func(*manager)

func WithSummaryStore(store SummaryStore) Option {
	return func(m *manager) { m.summaries = store }
}

func WithMemoryStore(store MemoryStore) Option {
	return func(m *manager) { m.memories = store }
}

func WithTaskStateStore(store TaskStateStore) Option {
	return func(m *manager) { m.taskStates = store }
}

func WithUserProfileStore(store UserProfileStore) Option {
	return func(m *manager) { m.userProfile = store }
}

func NewManager(messages MessageStore, tools ToolContextStore, opts ...Option) Manager {
	m := &manager{messages: messages, tools: tools}
	for _, opt := range opts {
		opt(m)
	}
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

	// 查询最近的未被压缩的消息记录
	rows, err := m.messages.FindRecent(ctx, req.UserID, req.ConversationID, recentMessageQueryLimit)
	if err != nil {
		return nil, fmt.Errorf("load recent context messages: %w", err)
	}
	var summary *domain.ConversationSummary
	if req.Mode == domain.AgentContextMode && m.summaries != nil {
		// 获取最新的被压缩的长期记忆
		summary, _ = m.summaries.FindLatest(ctx, req.UserID, req.ConversationID)
	}
	recent := selectRecentMessages(rows, req.CurrentMessageID, summary)
	result := &domain.BuildContextResult{
		Messages: make([]domain.ContextMessage, 0, len(recent)+8),
	}

	result.Messages = append(result.Messages, domain.ContextMessage{Role: domain.ContextRoleSystem, Content: agentprompt.SystemPrompt})
	appendSummary(summary, result)

	for _, row := range recent {
		result.Messages = append(result.Messages, domain.ContextMessage{Role: row.Role, Content: redactSensitiveContext(row.Content)})
	}
	if len(recent) > 0 {
		result.RecentMessageStartID = recent[0].MsgId
		result.RecentMessageEndID = recent[len(recent)-1].MsgId
	}

	m.appendToolContext(ctx, req, result)
	m.appendTaskState(ctx, req, result)
	m.appendAgentMemory(ctx, req, result)
	m.appendUserProfile(ctx, req, result)

	result.Messages = append(result.Messages, domain.ContextMessage{Role: domain.ContextRoleUser, Content: req.CurrentInput})
	result.EstimatedInputTokens = estimateInputTokens(result.Messages)
	return result, nil
}

var (
	sensitiveContextAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*=\s*[^\s,，;；]+`)
	sensitiveContextColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*[:：]\s*[^\s,，;；]+`)
)

func redactSensitiveContext(content string) string {
	content = sensitiveContextAssignmentPattern.ReplaceAllString(content, "$1=[redacted]")
	return sensitiveContextColonPattern.ReplaceAllString(content, "$1:[redacted]")
}

func appendSummary(summary *domain.ConversationSummary, result *domain.BuildContextResult) {
	if summary == nil {
		return
	}
	if message, ok := structuredContextMessage("conversation_summary", summary); ok {
		result.Messages = append(result.Messages, message)
		result.SummaryCoveredMessageID = summary.CoveredUntilMessageID
		result.SummaryCoveredUntilCreatedAt = summary.CoveredUntilCreatedAt
	}
}

func (m *manager) appendToolContext(ctx context.Context, req domain.BuildContextRequest, result *domain.BuildContextResult) {
	if m.tools == nil {
		return
	}
	latest, err := m.tools.FindLatestResult(ctx, req.UserID, req.ConversationID)
	if err == nil && latest != nil {
		if message, ok := structuredContextMessage("latest_tool_result", latest); ok {
			result.Messages = append(result.Messages, message)
			result.LatestToolCallID = latest.ToolCallID
		}
	}

	refs, err := m.tools.FindRecentRefs(ctx, req.UserID, req.ConversationID, recentToolRefLimit)
	if err != nil {
		return
	}
	historical := make([]domain.ToolCallRef, 0, len(refs))
	for _, ref := range refs {
		if ref.ToolCallID == result.LatestToolCallID {
			continue
		}
		historical = append(historical, ref)
	}
	if len(historical) == 0 {
		return
	}
	if message, ok := structuredContextMessage("tool_call_refs", historical); ok {
		result.Messages = append(result.Messages, message)
		result.ToolCallRefCount = len(historical)
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
		result.Messages = append(result.Messages, message)
	}
}

func (m *manager) appendAgentMemory(ctx context.Context, req domain.BuildContextRequest, result *domain.BuildContextResult) {
	if m.memories == nil {
		return
	}
	memories, err := m.memories.ListActive(ctx, req.UserID, activeMemoryLimit)
	if err != nil || len(memories) == 0 {
		return
	}
	if message, ok := structuredContextMessage("user_memories", memories); ok {
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
		result.Messages = append(result.Messages, message)
	}
}

func selectRecentMessages(rows []*aimessages.AiMessages, currentMessageID string, summary *domain.ConversationSummary) []*aimessages.AiMessages {
	recent := make([]*aimessages.AiMessages, 0, len(rows))
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
		recent = append(recent, row)
	}
	sort.SliceStable(recent, func(i, j int) bool {
		if recent[i].CreatedAt.Equal(recent[j].CreatedAt) {
			return recent[i].MsgId < recent[j].MsgId
		}
		return recent[i].CreatedAt.Before(recent[j].CreatedAt)
	})
	if len(recent) > recentMessageLimit {
		recent = recent[len(recent)-recentMessageLimit:]
	}
	return recent
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

func estimateInputTokens(messages []domain.ContextMessage) int {
	runes := 0
	for _, message := range messages {
		runes += utf8.RuneCountInString(message.Content)
	}
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}
