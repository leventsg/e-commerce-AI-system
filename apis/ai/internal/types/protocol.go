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
	Type            string          `json:"type"`
	ConversationID  string          `json:"conversation_id,omitempty"`
	Content         string          `json:"content,omitempty"`
	ClientMessageID string          `json:"client_message_id,omitempty"`
	Metadata        ClientMetadata  `json:"metadata,omitempty"`
	ConfirmationID  string          `json:"confirmation_id,omitempty"`
	Approved        *bool           `json:"approved,omitempty"`
	UserID          json.RawMessage `json:"user_id,omitempty"`
}

type ServerEvent struct {
	Type           string          `json:"type"`
	ConversationID string          `json:"conversation_id,omitempty"`
	MessageID      string          `json:"message_id,omitempty"`
	Content        string          `json:"content,omitempty"`
	Sources        []SourceInfo    `json:"sources,omitempty"`
	Tool           string          `json:"tool,omitempty"`
	Status         string          `json:"status,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	ConfirmationID string          `json:"confirmation_id,omitempty"`
	Action         string          `json:"action,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	ExpiresAt      int64           `json:"expires_at,omitempty"`
	Done           bool            `json:"done"`
}

type SourceInfo struct {
	DocumentID  string        `json:"document_id"`
	Title       string        `json:"title"`
	DocumentURL string        `json:"document_url,omitempty"`
	Chunks      []SourceChunk `json:"chunks"`
}

type SourceChunk struct {
	ChunkID string  `json:"chunk_id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}
