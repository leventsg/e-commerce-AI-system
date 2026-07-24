package domain

import (
	"encoding/json"
	"time"
)

type ContextMode string

const (
	IntentContextMode ContextMode = "intent"
	AgentContextMode  ContextMode = "agent"

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
	ToolCallRefCount             int
	EstimatedInputTokens         int
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

type UserMemory struct {
	Id              string     `json:"id,omitempty"`
	UserID          uint64     `json:"-"`
	Key             string     `json:"key"`
	Type            string     `json:"type,omitempty"`
	Content         string     `json:"content"`
	Confidence      float64    `json:"confidence,omitempty"`
	Source          string     `json:"source,omitempty"`
	SourceMessageID string     `json:"source_message_id,omitempty"`
	Status          string     `json:"status,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	LastConfirmedAt *time.Time `json:"last_confirmed_at,omitempty"`
}

type UserProfile struct {
	DisplayName string `json:"display_name,omitempty"`
	Locale      string `json:"locale,omitempty"`
}

const (
	MemoryTypeInstruction = "instruction"
	MemoryTypePreference  = "preference"
	MemoryTypePrice       = "price"
	MemoryTypeProfileFact = "profile_fact"

	MemorySourceExplicit = "explicit"
	MemorySourceInferred = "inferred"

	MemoryStatusActive     = "active"
	MemoryStatusSuperseded = "superseded"
	MemoryStatusDeleted    = "deleted"
	MemoryStatusExpired    = "expired"
)

// ToolResultEnvelope 工具调用结果封装
type ToolResultEnvelope struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Status     string          `json:"status"`
	Data       json.RawMessage `json:"data"`
	Summary    string          `json:"summary"`
	ExpiresAt  *time.Time      `json:"expires_at,omitempty"`
}

// ToolCallRef 工具调用引用
type ToolCallRef struct {
	ToolCallID string              `json:"tool_call_id"`
	ToolName   string              `json:"tool_name"`
	Status     string              `json:"status"`
	Summary    string              `json:"summary"`
	EntityIDs  map[string][]string `json:"entity_ids,omitempty"`
	State      map[string]any      `json:"state,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
}
