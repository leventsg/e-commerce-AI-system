package memory

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

// MemoryProvider abstracts model-context retrieval and post-response memory work.
// The customer-service implementation reads existing conversation, summary, tool,
// memory, and profile stores;
type MemoryProvider interface {
	Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResult, error)
	Memorize(ctx context.Context, req *MemorizeRequest) error
	Close() error
}

type RetrieveRequest struct {
	UserID           uint64
	ConversationID   string
	RunID            string
	CurrentMessageID string
	ClientMessageID  string
	Messages         []domain.ContextMessage
	Limit            int
}

type RetrieveResult struct {
	SystemMessages  []domain.ContextMessage
	HistoryMessages []domain.ContextMessage
	ContextMessages []domain.ContextMessage
	Metadata        map[string]any
}

type MemorizeRequest struct {
	UserID          uint64
	ConversationID  string
	RunID           string
	ClientMessageID string
	Messages        []domain.ContextMessage
	MessageIDs      []string
}

type Event struct {
	UserID    uint64
	Type      string
	EventDate string
	Summary   string
	Keywords  []string
}

type UserMemoryEventQuery struct {
	UserID   uint64
	Keywords []string
	Match    string
	Type     string
	Since    string
	Until    string
	Limit    int
}

type ToolCallMemory struct {
	ToolName          string
	ToolCallID        string
	ToolCallArguments string
}

// UserMemoryEventSearcher provides optional long-term memory event lookup.
// Tools must set UserID from trusted session metadata rather than model input.
type UserMemoryEventSearcher interface {
	SearchUserMemoryEvents(ctx context.Context, query UserMemoryEventQuery) ([]Event, error)
}
