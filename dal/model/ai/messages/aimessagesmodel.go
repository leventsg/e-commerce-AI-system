package messages

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiMessagesModel = (*customAiMessagesModel)(nil)

type (
	// AiMessagesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiMessagesModel.
	AiMessagesModel interface {
		aiMessagesModel
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
