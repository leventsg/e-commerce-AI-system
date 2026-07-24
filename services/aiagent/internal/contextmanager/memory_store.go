package contextmanager

import (
	"context"
	"database/sql"
	"strings"
	"time"

	aiusermemories "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_memories"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type memoryModel interface {
	FindOneByUserIdMemoryKey(ctx context.Context, userId uint64, memoryKey string) (*aiusermemories.AiUserMemories, error)
	FindActiveByUser(ctx context.Context, userID uint64, limit int, now time.Time) ([]*aiusermemories.AiUserMemories, error)
	UpsertByKey(ctx context.Context, data *aiusermemories.AiUserMemories) (*aiusermemories.AiUserMemories, error)
	ExpireDue(ctx context.Context, userID uint64, now time.Time) (int, error)
}

type MemoryModelStore struct {
	model memoryModel
	now   func() time.Time
}

func NewMemoryStore(model memoryModel) *MemoryModelStore {
	return &MemoryModelStore{model: model, now: time.Now}
}

func (s *MemoryModelStore) FindByKey(ctx context.Context, userID uint64, key string) (*domain.UserMemory, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	row, err := s.model.FindOneByUserIdMemoryKey(ctx, userID, key)
	if err != nil || row == nil {
		return nil, nil
	}
	return userMemoryFromRow(row), nil
}

func (s *MemoryModelStore) Upsert(ctx context.Context, memory *domain.UserMemory) (*domain.UserMemory, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	row := userMemoryToRow(memory)
	saved, err := s.model.UpsertByKey(ctx, row)
	if err != nil {
		return nil, err
	}
	return userMemoryFromRow(saved), nil
}

func (s *MemoryModelStore) ListActive(ctx context.Context, userID uint64, limit int) ([]domain.UserMemory, error) {
	now := time.Now()
	if s != nil && s.now != nil {
		now = s.now()
	}
	return s.ListActiveAt(ctx, userID, limit, now)
}

func (s *MemoryModelStore) ListActiveAt(ctx context.Context, userID uint64, limit int, now time.Time) ([]domain.UserMemory, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	rows, err := s.model.FindActiveByUser(ctx, userID, limit, now)
	if err != nil {
		return nil, err
	}
	result := make([]domain.UserMemory, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			result = append(result, *userMemoryFromRow(row))
		}
	}
	return result, nil
}

func (s *MemoryModelStore) SummarizeForIntent(ctx context.Context, userID uint64, limit int) (string, error) {
	memories, err := s.ListActive(ctx, userID, limit)
	if err != nil || len(memories) == 0 {
		return "", err
	}
	parts := make([]string, 0, len(memories))
	for _, memory := range memories {
		parts = append(parts, memory.Type+":"+memory.Content)
	}
	return strings.Join(parts, "\n"), nil
}

func (s *MemoryModelStore) ExpireDue(ctx context.Context, userID uint64, now time.Time) (int, error) {
	if s == nil || s.model == nil {
		return 0, ErrContextManagerUnavailable
	}
	return s.model.ExpireDue(ctx, userID, now)
}

func userMemoryFromRow(row *aiusermemories.AiUserMemories) *domain.UserMemory {
	return &domain.UserMemory{
		Id:              row.Id,
		UserID:          row.UserId,
		Key:             row.MemoryKey,
		Type:            row.MemoryType,
		Content:         row.Content,
		Confidence:      row.Confidence,
		Source:          row.Source,
		SourceMessageID: row.SourceMessageId,
		Status:          row.Status,
		ExpiresAt:       nullTimePtr(row.ExpiresAt),
		LastConfirmedAt: nullTimePtr(row.LastConfirmedAt),
	}
}

func userMemoryToRow(memory *domain.UserMemory) *aiusermemories.AiUserMemories {
	return &aiusermemories.AiUserMemories{
		Id:              memory.Id,
		UserId:          memory.UserID,
		MemoryKey:       memory.Key,
		MemoryType:      memory.Type,
		Content:         memory.Content,
		Confidence:      memory.Confidence,
		Source:          memory.Source,
		SourceMessageId: memory.SourceMessageID,
		Status:          memory.Status,
		ExpiresAt:       ptrToNullTime(memory.ExpiresAt),
		LastConfirmedAt: ptrToNullTime(memory.LastConfirmedAt),
	}
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func ptrToNullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *value, Valid: true}
}
