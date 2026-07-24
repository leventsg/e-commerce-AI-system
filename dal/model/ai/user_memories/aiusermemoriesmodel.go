package user_memories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiUserMemoriesModel = (*customAiUserMemoriesModel)(nil)

type (
	// AiUserMemoriesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiUserMemoriesModel.
	AiUserMemoriesModel interface {
		aiUserMemoriesModel
		FindActiveByUser(ctx context.Context, userID uint64, limit int, now time.Time) ([]*AiUserMemories, error)
		UpsertByKey(ctx context.Context, data *AiUserMemories) (*AiUserMemories, error)
		ExpireDue(ctx context.Context, userID uint64, now time.Time) (int, error)
	}

	customAiUserMemoriesModel struct {
		*defaultAiUserMemoriesModel
	}
)

func (m *customAiUserMemoriesModel) FindActiveByUser(ctx context.Context, userID uint64, limit int, now time.Time) ([]*AiUserMemories, error) {
	if limit <= 0 {
		limit = 12
	}
	var rows []*AiUserMemories
	query := fmt.Sprintf("select %s from %s where `user_id` = ? and `status` = 'active' and (`expires_at` is null or `expires_at` > ?) order by `last_confirmed_at` desc, `updated_at` desc limit ?", aiUserMemoriesRows, m.table)
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, userID, now, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *customAiUserMemoriesModel) UpsertByKey(ctx context.Context, data *AiUserMemories) (*AiUserMemories, error) {
	existing, err := m.FindOneByUserIdMemoryKey(ctx, data.UserId, data.MemoryKey)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) && !errors.Is(err, sqlc.ErrNotFound) {
		return nil, err
	}
	if existing == nil || errors.Is(err, sqlx.ErrNotFound) || errors.Is(err, sqlc.ErrNotFound) {
		if _, err := m.Insert(ctx, data); err != nil {
			return nil, err
		}
		return data, nil
	}
	data.Id = existing.Id
	if err := m.Update(ctx, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (m *customAiUserMemoriesModel) ExpireDue(ctx context.Context, userID uint64, now time.Time) (int, error) {
	query := fmt.Sprintf("update %s set `status` = 'expired' where `user_id` = ? and `status` = 'active' and `expires_at` is not null and `expires_at` <= ?", m.table)
	result, err := m.CachedConn.ExecNoCacheCtx(ctx, query, userID, now)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// NewAiUserMemoriesModel returns a model for the database table.
func NewAiUserMemoriesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiUserMemoriesModel {
	return &customAiUserMemoriesModel{
		defaultAiUserMemoriesModel: newAiUserMemoriesModel(conn, c, opts...),
	}
}
