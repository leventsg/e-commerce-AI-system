package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const (
	defaultRecentMessageLimit = 20
	defaultRecentToolRefLimit = 20
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

type SummaryRefresher interface {
	RefreshMemorySummary(ctx context.Context, userID uint64, conversationID string) error
}

type ProfileUpdatePublisher interface {
	PublishMemoryUpdate(ctx context.Context, userID uint64, conversationID string, messageIDs []string) error
}

type MemoryEventStore interface {
	ListRecentUserMemoryEvents(ctx context.Context, userID uint64, limit int) ([]Event, error)
	SearchUserMemoryEvents(ctx context.Context, query UserMemoryEventQuery) ([]Event, error)
}

type CustomerServiceProviderConfig struct {
	Messages         MessageStore
	Summaries        SummaryStore
	Tools            ToolContextStore
	TaskStates       TaskStateStore
	Profiles         UserProfileStore
	Events           MemoryEventStore
	SummaryRefresher SummaryRefresher
	ProfilePublisher ProfileUpdatePublisher
	Now              func() time.Time
}

type CustomerServiceProvider struct {
	messages         MessageStore
	summaries        SummaryStore
	tools            ToolContextStore
	taskStates       TaskStateStore
	profiles         UserProfileStore
	events           MemoryEventStore
	summaryRefresher SummaryRefresher
	profilePublisher ProfileUpdatePublisher
	now              func() time.Time
}

func NewCustomerServiceProvider(cfg CustomerServiceProviderConfig) *CustomerServiceProvider {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &CustomerServiceProvider{
		messages:         cfg.Messages,
		summaries:        cfg.Summaries,
		tools:            cfg.Tools,
		taskStates:       cfg.TaskStates,
		profiles:         cfg.Profiles,
		events:           cfg.Events,
		summaryRefresher: cfg.SummaryRefresher,
		profilePublisher: cfg.ProfilePublisher,
		now:              now,
	}
}

func (p *CustomerServiceProvider) Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResult, error) {
	if req == nil || req.UserID == 0 || req.ConversationID == "" {
		return &RetrieveResult{Metadata: map[string]any{}}, nil
	}
	result := &RetrieveResult{Metadata: map[string]any{}}
	var summary *domain.ConversationSummary
	// 获取会话摘要
	if p.summaries != nil {
		// 获取最新的会话摘要信息，如果没有则返回nil
		if loaded, err := p.summaries.FindLatest(ctx, req.UserID, req.ConversationID); err == nil {
			summary = loaded
			if summary != nil {
				result.Metadata["summary_covered_message_id"] = summary.CoveredUntilMessageID
			}
		} else {
			result.Metadata["summary_degraded"] = err.Error()
		}
	}
	// 获取最近的消息记录
	if p.messages != nil {
		limit := defaultRecentMessageLimit + 1
		if req.Limit > 0 {
			limit = req.Limit + 1
		}
		if rows, err := p.messages.FindRecent(ctx, req.UserID, req.ConversationID, limit); err == nil {
			result.HistoryMessages = historyMessages(rows, req.CurrentMessageID, summary, defaultRecentMessageLimit)
			if len(result.HistoryMessages) > 0 {
				result.Metadata["recent_message_count"] = len(result.HistoryMessages)
			}
		} else {
			result.Metadata["history_degraded"] = err.Error()
		}
	}
	// 当前时间注入上下文
	contextBlocks := make([]domain.ContextMessage, 0, 8)
	contextBlocks = append(contextBlocks, domain.ContextMessage{Role: domain.ContextRoleUser, Content: formatBlock("current_time", p.now().Format(time.RFC3339))})
	// 将摘要信息作为上下文块注入
	if summary != nil {
		if msg, ok := jsonBlock("conversation_context", summary); ok {
			contextBlocks = append(contextBlocks, msg)
		}
	}
	// 工具调用结果
	p.appendToolContext(ctx, req, &contextBlocks, result.Metadata)
	// 当前任务状态
	p.appendTaskState(ctx, req, &contextBlocks, result.Metadata)
	// 用户事件
	p.appendEvents(ctx, req, &contextBlocks, result.Metadata)
	// 用户画像
	p.appendProfile(ctx, req, &contextBlocks, result.Metadata)
	// 最终上下文块
	result.ContextMessages = contextBlocks
	return result, nil
}

func (p *CustomerServiceProvider) Memorize(ctx context.Context, req *MemorizeRequest) error {
	if req == nil || req.UserID == 0 || req.ConversationID == "" {
		return nil
	}
	if len(req.Messages) < 2 || len(req.MessageIDs) < 2 {
		return nil
	}
	if p.summaryRefresher != nil {
		if err := p.summaryRefresher.RefreshMemorySummary(ctx, req.UserID, req.ConversationID); err != nil {
			return err
		}
	}
	if p.profilePublisher != nil {
		return p.profilePublisher.PublishMemoryUpdate(ctx, req.UserID, req.ConversationID, req.MessageIDs)
	}
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

// 获取工具调用结果
func (p *CustomerServiceProvider) appendToolContext(ctx context.Context, req *RetrieveRequest, blocks *[]domain.ContextMessage, meta map[string]any) {
	if p.tools == nil {
		return
	}
	// 获取最新的工具调用结果
	latest, err := p.tools.FindLatestToolResult(ctx, req.UserID, req.ConversationID)
	if err == nil && latest != nil {
		if msg, ok := jsonBlock("latest_tool_result", latest); ok {
			*blocks = append(*blocks, msg)
			meta["latest_tool_call_id"] = latest.ToolCallId
		}
	} else if err != nil {
		meta["tool_result_degraded"] = err.Error()
	}
	// 获取最近的toolcall
	recent, err := p.tools.FindRecentToolCall(ctx, req.UserID, req.ConversationID, defaultRecentToolRefLimit)
	if err != nil {
		meta["recent_tool_calls_degraded"] = err.Error()
		return
	}
	historical := make([]*ToolCallMemory, 0, len(recent))
	// 只保存最小call记录
	for _, call := range recent {
		if call == nil || (latest != nil && call.ToolCallId == latest.ToolCallId) {
			continue
		}
		temp := &ToolCallMemory{
			ToolName:          call.ToolName,
			ToolCallID:        call.ToolCallId,
			ToolCallArguments: call.Arguments,
		}
		historical = append(historical, temp)
	}
	if len(historical) == 0 {
		return
	}
	if msg, ok := jsonBlock("recent_tool_calls", historical); ok {
		*blocks = append(*blocks, msg)
		meta["recent_tool_call_count"] = len(historical)
	}
}

func (p *CustomerServiceProvider) appendTaskState(ctx context.Context, req *RetrieveRequest, blocks *[]domain.ContextMessage, meta map[string]any) {
	if p.taskStates == nil || req.RunID == "" {
		return
	}
	state, err := p.taskStates.FindActive(ctx, req.UserID, req.ConversationID, req.RunID)
	if err != nil {
		meta["task_state_degraded"] = err.Error()
		return
	}
	if state == nil {
		return
	}
	if msg, ok := jsonBlock("task_state", state); ok {
		*blocks = append(*blocks, msg)
	}
}

func (p *CustomerServiceProvider) appendEvents(ctx context.Context, req *RetrieveRequest, blocks *[]domain.ContextMessage, meta map[string]any) {
	if p.events == nil {
		return
	}
	events, err := p.events.ListRecentUserMemoryEvents(ctx, req.UserID, defaultRecentToolRefLimit)
	if err != nil {
		meta["memory_events_degraded"] = err.Error()
		return
	}
	if len(events) == 0 {
		return
	}
	if msg, ok := jsonBlock("user_memory_recent_events", events); ok {
		*blocks = append(*blocks, msg)
		meta["user_memory_event_count"] = len(events)
	}
}

func (p *CustomerServiceProvider) appendProfile(ctx context.Context, req *RetrieveRequest, blocks *[]domain.ContextMessage, meta map[string]any) {
	if p.profiles == nil {
		return
	}
	profile, err := p.profiles.LoadActive(ctx, req.UserID)
	if err != nil {
		meta["profile_degraded"] = err.Error()
		return
	}
	if profile == nil {
		return
	}
	if msg, ok := jsonBlock("user_profile", profile.ProfileJSON); ok {
		*blocks = append(*blocks, msg)
	}
}

func historyMessages(rows []*aimessages.AiMessages, currentMessageID string, summary *domain.ConversationSummary, limit int) []domain.ContextMessage {
	filtered := make([]*aimessages.AiMessages, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.MsgId == currentMessageID || strings.TrimSpace(row.Content) == "" {
			continue
		}
		// 过滤tool消息
		if row.Role != domain.ContextRoleUser && row.Role != domain.ContextRoleAssistant {
			continue
		}
		// 过滤掉被摘要覆盖的消息
		if coveredBySummary(row, summary) {
			continue
		}
		filtered = append(filtered, row)
	}
	// 按创建时间排序
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].MsgId < filtered[j].MsgId
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	// 截断数量，最多返回最近的 limit 条
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	result := make([]domain.ContextMessage, 0, len(filtered))
	for _, row := range filtered {
		result = append(result, domain.ContextMessage{Role: row.Role, Content: redactSensitiveContext(row.Content)})
	}
	return result
}

// 判断消息是否被摘要覆盖
func coveredBySummary(message *aimessages.AiMessages, summary *domain.ConversationSummary) bool {
	if message == nil || summary == nil || summary.CoveredUntilCreatedAt.IsZero() {
		return false
	}
	// 消息时间 < 摘要截止时间？
	if message.CreatedAt.Before(summary.CoveredUntilCreatedAt) {
		return true
	}
	// 消息时间 == 摘要截止时间 && 消息ID <= 摘要截止ID？
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
