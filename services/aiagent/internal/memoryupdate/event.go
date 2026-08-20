package memoryupdate

import (
	"time"
)

const TopicKeyAiMemoryUpdates = "AiMemoryUpdates"

// UpdateEvent is published once after a rolling summary compresses messages.
type UpdateEvent struct {
	EventID        string    `json:"event_id"`
	UserID         uint64    `json:"user_id"`
	ConversationID string    `json:"conversation_id"`
	MessageIDs     []string  `json:"message_ids"`
	CreatedAt      time.Time `json:"created_at"`
}
