package logic

import (
	"context"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
)

func TestUpdateConversationMemoryIgnoresRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	messages := &memoryContextMessagesStore{}
	logic := &ChatLogic{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{
			SummaryManager: contextmanager.NewSummaryManager(
				&memoryContextSummaryStore{},
				messages,
				nil,
			),
			MemoryUpdatePublisher: &memoryupdate.KafkaPublisher{},
		},
	}

	logic.updateConversationMemory(&conversation.PreparedConversation{ConversationID: "conv-1"}, 42)

	if messages.countCtxErr != nil {
		t.Fatalf("summary refresh context error = %v, want nil", messages.countCtxErr)
	}
}

type memoryContextSummaryStore struct{}

func (s *memoryContextSummaryStore) FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error) {
	return nil, nil
}

func (s *memoryContextSummaryStore) Save(ctx context.Context, userID uint64, conversationID string, summary *domain.ConversationSummary) error {
	return nil
}

type memoryContextMessagesStore struct {
	countCtxErr error
}

func (s *memoryContextMessagesStore) CountUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string) (int64, error) {
	s.countCtxErr = ctx.Err()
	return 0, nil
}

func (s *memoryContextMessagesStore) FindUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}

func (s *memoryContextMessagesStore) FindRecentUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}
