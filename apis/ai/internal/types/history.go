package types

import "encoding/json"

type ListSessionsRequest struct {
	Page     int64 `form:"page,optional"`
	PageSize int64 `form:"page_size,optional"`
}

type SessionSummary struct {
	ConversationID     string `json:"conversation_id"`
	Title              string `json:"title"`
	LastMessagePreview string `json:"last_message_preview,omitempty"`
	UpdatedAt          string `json:"updated_at"`
	MessageCount       int64  `json:"message_count"`
}

type ListSessionsResponse struct {
	Total         int64            `json:"total"`
	Conversations []SessionSummary `json:"conversations"`
}

type ListMessagesRequest struct {
	ConversationID string `form:"conversation_id"`
	Page           int64  `form:"page,optional"`
	PageSize       int64  `form:"page_size,optional"`
}

type HistoryMessage struct {
	MessageID       string          `json:"message_id"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	ClientMessageID string          `json:"client_message_id,omitempty"`
	CreatedAt       string          `json:"created_at"`
}

type ListMessagesResponse struct {
	ConversationID string           `json:"conversation_id"`
	Total          int64            `json:"total"`
	Messages       []HistoryMessage `json:"messages"`
}
