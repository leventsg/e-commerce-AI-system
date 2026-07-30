package conversation_summaries

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiConversationSummariesModel = (*customAiConversationSummariesModel)(nil)

type (
	// AiConversationSummariesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiConversationSummariesModel.
	AiConversationSummariesModel interface {
		aiConversationSummariesModel
		FindLatestByUserConversation(ctx context.Context, userID uint64, conversationID string) (*AiConversationSummaries, error)
	}

	customAiConversationSummariesModel struct {
		*defaultAiConversationSummariesModel
	}
)

func (m *customAiConversationSummariesModel) FindLatestByUserConversation(ctx context.Context, userID uint64, conversationID string) (*AiConversationSummaries, error) {
	var row AiConversationSummaries
	query := fmt.Sprintf("select %s from %s where `user_id` = ? and `conversation_id` = ? order by `covered_until_created_at` desc, `covered_until_message_id` desc limit 1", aiConversationSummariesRows, m.table)
	err := m.CachedConn.QueryRowNoCacheCtx(ctx, &row, query, userID, conversationID)
	if errors.Is(err, sqlc.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// NewAiConversationSummariesModel returns a model for the database table.
func NewAiConversationSummariesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiConversationSummariesModel {
	return &customAiConversationSummariesModel{
		defaultAiConversationSummariesModel: newAiConversationSummariesModel(conn, c, opts...),
	}
}
