package messages

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiMessagesModel = (*customAiMessagesModel)(nil)

type (
	// AiMessagesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiMessagesModel.
	AiMessagesModel interface {
		aiMessagesModel
		FindRecentByConversationID(ctx context.Context, conversationID string, limit int) ([]*AiMessages, error)
	}

	customAiMessagesModel struct {
		*defaultAiMessagesModel
	}
)

// NewAiMessagesModel returns a model for the database table.
func NewAiMessagesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiMessagesModel {
	return &customAiMessagesModel{
		defaultAiMessagesModel: newAiMessagesModel(conn, c, opts...),
	}
}

func (m *customAiMessagesModel) FindRecentByConversationID(ctx context.Context, conversationID string, limit int) ([]*AiMessages, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `conversation_id` = ? order by `created_at` desc limit ?"
	err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, conversationID, limit)
	if err != nil {
		return nil, err
	}

	// 交换顺序，由新到旧排序
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, nil
}
