package domain

import (
	"encoding/json"
	"time"
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
