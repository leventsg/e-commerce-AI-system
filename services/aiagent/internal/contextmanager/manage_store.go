package contextmanager

import (
	"context"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
)

type contextMessagesModel interface {
	FindRecentContextMessages(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error)
}

type MessageStore interface {
	FindRecent(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error)
}

type messageStore struct {
	model contextMessagesModel
}

func NewMessageStore(model contextMessagesModel) MessageStore {
	return &messageStore{model: model}
}

func (s *messageStore) FindRecent(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	return s.model.FindRecentContextMessages(ctx, userID, conversationID, limit)
}
