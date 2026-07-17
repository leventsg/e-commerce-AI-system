package domain

import "time"

type Confirmation struct {
	ID             string
	ConversationID string
	UserID         uint64
	ToolName       string
	Arguments      map[string]any
	Summary        string
	Status         string
	ExpiresAt      time.Time
	ExecutedAt     *time.Time
}
