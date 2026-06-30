package conversations

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiConversationsModel = (*customAiConversationsModel)(nil)

type (
	// AiConversationsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiConversationsModel.
	AiConversationsModel interface {
		aiConversationsModel
	}

	customAiConversationsModel struct {
		*defaultAiConversationsModel
	}
)

// NewAiConversationsModel returns a model for the database table.
func NewAiConversationsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiConversationsModel {
	return &customAiConversationsModel{
		defaultAiConversationsModel: newAiConversationsModel(conn, c, opts...),
	}
}
