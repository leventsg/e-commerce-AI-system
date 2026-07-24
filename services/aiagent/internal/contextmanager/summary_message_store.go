package contextmanager

import (
	"context"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
)

type unsummarizedMessagesModel interface {
	CountUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string) (int64, error)
	FindUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string, limit int) ([]*aimessages.AiMessages, error)
	FindRecentUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string, limit int) ([]*aimessages.AiMessages, error)
}

type SummaryMessageStore struct {
	model unsummarizedMessagesModel
}

func NewSummaryMessageStore(model unsummarizedMessagesModel) *SummaryMessageStore {
	return &SummaryMessageStore{model: model}
}

func (s *SummaryMessageStore) CountUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string) (int64, error) {
	if s == nil || s.model == nil {
		return 0, ErrContextManagerUnavailable
	}
	watermark := ""
	if !afterCreatedAt.IsZero() {
		watermark = afterCreatedAt.Format("2006-01-02 15:04:05.000")
	}
	return s.model.CountUnsummarizedContextMessages(ctx, userID, conversationID, watermark, afterMessageID)
}

// FindUnsummarized 查找未摘要压缩的消息
func (s *SummaryMessageStore) FindUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	watermark := ""
	if !afterCreatedAt.IsZero() {
		watermark = afterCreatedAt.Format("2006-01-02 15:04:05.000")
	}
	return s.model.FindUnsummarizedContextMessages(ctx, userID, conversationID, watermark, afterMessageID, limit)
}

// FindRecentUnsummarized 查找最近的未摘要压缩的消息，默认返回最近的 20 条消息
func (s *SummaryMessageStore) FindRecentUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	watermark := ""
	if !afterCreatedAt.IsZero() {
		watermark = afterCreatedAt.Format("2006-01-02 15:04:05.000")
	}
	return s.model.FindRecentUnsummarizedContextMessages(ctx, userID, conversationID, watermark, afterMessageID, limit)
}
