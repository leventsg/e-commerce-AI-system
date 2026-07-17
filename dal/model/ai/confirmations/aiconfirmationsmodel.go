package confirmations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var ErrInvalidStatusTransition = errors.New("invalid confirmation status transition")

var _ AiConfirmationsModel = (*customAiConfirmationsModel)(nil)

type (
	// AiConfirmationsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiConfirmationsModel.
	AiConfirmationsModel interface {
		aiConfirmationsModel
		FindOneUncached(ctx context.Context, id string) (*AiConfirmations, error)
		ResolvePending(ctx context.Context, id string, userID uint64, nextStatus string, now time.Time) (bool, error)
		ExpirePending(ctx context.Context, id string, userID uint64, now time.Time) (bool, error)
		CompleteApproved(ctx context.Context, id string, userID uint64, nextStatus string, executedAt time.Time) (bool, error)
	}

	customAiConfirmationsModel struct {
		*defaultAiConfirmationsModel
	}
)

func (m *customAiConfirmationsModel) FindOneUncached(ctx context.Context, id string) (*AiConfirmations, error) {
	var confirmation AiConfirmations
	query := fmt.Sprintf("select %s from %s where `id` = ? limit 1", aiConfirmationsRows, m.table)
	err := m.CachedConn.QueryRowNoCacheCtx(ctx, &confirmation, query, id)
	switch {
	case err == nil:
		return &confirmation, nil
	case errors.Is(err, sqlx.ErrNotFound):
		return nil, ErrNotFound
	default:
		return nil, err
	}
}

func (m *customAiConfirmationsModel) ResolvePending(ctx context.Context, id string, userID uint64, nextStatus string, now time.Time) (bool, error) {
	if nextStatus != "approved" && nextStatus != "rejected" {
		return false, ErrInvalidStatusTransition
	}
	query := fmt.Sprintf("update %s set `status` = ? where `id` = ? and `user_id` = ? and `status` = 'pending' and `expires_at` > ?", m.table)
	return m.execConditionalUpdate(ctx, id, query, nextStatus, id, userID, now)
}

func (m *customAiConfirmationsModel) ExpirePending(ctx context.Context, id string, userID uint64, now time.Time) (bool, error) {
	query := fmt.Sprintf("update %s set `status` = 'expired' where `id` = ? and `user_id` = ? and `status` = 'pending' and `expires_at` <= ?", m.table)
	return m.execConditionalUpdate(ctx, id, query, id, userID, now)
}

func (m *customAiConfirmationsModel) CompleteApproved(ctx context.Context, id string, userID uint64, nextStatus string, executedAt time.Time) (bool, error) {
	if nextStatus != "executed" && nextStatus != "failed" {
		return false, ErrInvalidStatusTransition
	}
	query := fmt.Sprintf("update %s set `status` = ?, `executed_at` = ? where `id` = ? and `user_id` = ? and `status` = 'approved'", m.table)
	return m.execConditionalUpdate(ctx, id, query, nextStatus, executedAt, id, userID)
}

func (m *customAiConfirmationsModel) execConditionalUpdate(ctx context.Context, id, query string, args ...any) (bool, error) {
	cacheKey := fmt.Sprintf("%s%v", cacheAiConfirmationsIdPrefix, id)
	result, err := m.CachedConn.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx, query, args...)
	}, cacheKey)
	if err != nil && result != nil {
		logx.WithContext(ctx).Errorw("confirmation cache invalidation failed after committed update",
			logx.Field("confirmation_id", id), logx.Field("err", err))
	}
	return conditionalUpdateResult(result, err)
}

func conditionalUpdateResult(result sql.Result, execErr error) (bool, error) {
	if result == nil {
		return false, execErr
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// NewAiConfirmationsModel returns a model for the database table.
func NewAiConfirmationsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiConfirmationsModel {
	return &customAiConfirmationsModel{
		defaultAiConfirmationsModel: newAiConfirmationsModel(conn, c, opts...),
	}
}
