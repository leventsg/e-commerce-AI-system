package types

import "encoding/json"

const (
	ClientEventUserMessage   = "user_message"
	ClientEventConfirmAction = "confirm_action"
)

type ClientMetadata struct {
	Source string `json:"source,omitempty"`
}

type ClientMessage struct {
	Type           string          `json:"type"`
	Content        string          `json:"content,omitempty"`
	Metadata       ClientMetadata  `json:"metadata,omitempty"`
	ConfirmationID string          `json:"confirmation_id,omitempty"`
	Approved       *bool           `json:"approved,omitempty"`
	UserID         json.RawMessage `json:"user_id,omitempty"`
}

type ServerEvent struct {
	Type           string          `json:"type"`
	ConversationID string          `json:"conversation_id,omitempty"`
	MessageID      string          `json:"message_id,omitempty"`
	Content        string          `json:"content,omitempty"`
	Tool           string          `json:"tool,omitempty"`
	Status         string          `json:"status,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	ConfirmationID string          `json:"confirmation_id,omitempty"`
	Action         string          `json:"action,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	ExpiresAt      int64           `json:"expires_at,omitempty"`
	Done           bool            `json:"done"`
}
