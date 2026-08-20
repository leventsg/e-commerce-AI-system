package rag

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type Decision struct {
	NeedRAG    bool    `json:"need_rag"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

type PrepareRequest struct {
	UserID           uint64
	ConversationID   string
	RunID            string
	CurrentMessageID string
	ClientMessageID  string
	Content          EmbeddingRequest
}

type EmbeddingRequest struct {
	Query          string `json:"query"`
	EmbeddingModel string `json:"embeddingModel"`
}

type PrepareResult struct {
	ContextMessages []domain.ContextMessage
	Sources         []domain.AgentSource
}

type ConversationContextReader interface {
	FindRecent(ctx context.Context, userID uint64, conversationID string, limit int) ([]*messages.AiMessages, error)
}

type ConversationSummaryReader interface {
	FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error)
}
