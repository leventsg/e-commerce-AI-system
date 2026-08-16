package tool_calls

import (
	"context"
	"database/sql"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiToolCallsModel = (*customAiToolCallsModel)(nil)

type (
	// AiToolCallsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiToolCallsModel.
	AiToolCallsModel interface {
		aiToolCallsModel
		FindRecentSuccessfulToolCalls(ctx context.Context, userID uint64, conversationID string, limit int) ([]*AiToolCalls, error)
		FindToolCallByCallID(ctx context.Context, userID uint64, conversationID, toolCallID string) (*AiToolCalls, error)
	}

	customAiToolCallsModel struct {
		*defaultAiToolCallsModel
	}
)

// NewAiToolCallsModel returns a model for the database table.
func NewAiToolCallsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiToolCallsModel {
	return &customAiToolCallsModel{
		defaultAiToolCallsModel: newAiToolCallsModel(conn, c, opts...),
	}
}

func (m *customAiToolCallsModel) FindRecentSuccessfulToolCalls(ctx context.Context, userID uint64, conversationID string, limit int) ([]*AiToolCalls, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []*AiToolCalls
	query := "select " + aiToolCallsRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `status` = ? order by `id` desc limit ?"
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, userID, conversationID, "success", limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *customAiToolCallsModel) FindToolCallByCallID(ctx context.Context, userID uint64, conversationID, toolCallID string) (*AiToolCalls, error) {
	var row AiToolCalls
	query := "select " + aiToolCallsRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `tool_call_id` = ? limit 1"
	if err := m.CachedConn.QueryRowNoCacheCtx(ctx, &row, query, userID, conversationID, toolCallID); err != nil {
		if err == sqlx.ErrNotFound || err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}
