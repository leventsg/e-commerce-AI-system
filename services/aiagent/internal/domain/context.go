package domain

import (
	"encoding/json"
	"time"
)

type ContextMode string

const (
	AgentContextMode ContextMode = "agent"

	ContextRoleSystem    = "system"
	ContextRoleUser      = "user"
	ContextRoleAssistant = "assistant"
	ContextRoleTool      = "tool"
)

// BuildContextRequest 描述一次临时模型上下文构建请求。
// UserID 必须来自认证上下文，CurrentMessageID 用于避免重复注入刚持久化的当前输入。
type BuildContextRequest struct {
	UserID           uint64
	ConversationID   string
	RunID            string
	Mode             ContextMode
	CurrentMessageID string
	CurrentInput     string
}

// ContextMessage 是 Eino 边界之外使用的领域消息。
type ContextMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolName   string
}

// BuildContextResult 是本次临时组装结果及轻量观测元数据，不落库模型输入。
type BuildContextResult struct {
	Messages                     []ContextMessage
	SummaryCoveredMessageID      string
	SummaryCoveredUntilCreatedAt time.Time
	RecentMessageStartID         string
	RecentMessageEndID           string
	LatestToolCallID             string
	RecentToolCallCount          int
	EstimatedInputTokens         int
	// 以下为 token 预算观测字段：用于在 Langfuse/日志中记录预算使用情况，供后续调参。
	BudgetTotalTokens     int // 输入侧总预算（已乘安全边际）
	BudgetUsedTokens      int // 本次实际估算用量
	BudgetRemainingTokens int // 剩余预算（>=0）
	SpilledMessageCount   int // 因预算溢出未注入、待摘要覆盖的旧消息条数
}

type ConversationSummary struct {
	Summary               string         `json:"summary"`
	KeyFacts              map[string]any `json:"key_facts,omitempty"`
	OpenTasks             []string       `json:"open_tasks,omitempty"`
	CoveredUntilMessageID string         `json:"-"`
	CoveredUntilCreatedAt time.Time      `json:"-"`
	TokenCount            int            `json:"-"`
}

type TaskState struct {
	Goal                  string         `json:"goal,omitempty"`
	Parameters            map[string]any `json:"parameters,omitempty"`
	MissingParameters     []string       `json:"missing_parameters,omitempty"`
	CompletedSteps        []string       `json:"completed_steps,omitempty"`
	PendingToolCalls      []string       `json:"pending_tool_calls,omitempty"`
	PendingConfirmationID string         `json:"pending_confirmation_id,omitempty"`
	LastError             string         `json:"last_error,omitempty"`
}

type UserProfile struct {
	ProfileJSON json.RawMessage `json:"profile_json"`
	Version     uint64          `json:"version,omitempty"`
	LastEventID string          `json:"last_event_id,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at,omitempty"`
}
