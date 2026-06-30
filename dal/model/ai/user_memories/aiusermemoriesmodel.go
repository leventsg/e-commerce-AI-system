package user_memories

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiUserMemoriesModel = (*customAiUserMemoriesModel)(nil)

type (
	// AiUserMemoriesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiUserMemoriesModel.
	AiUserMemoriesModel interface {
		aiUserMemoriesModel
	}

	customAiUserMemoriesModel struct {
		*defaultAiUserMemoriesModel
	}
)

// NewAiUserMemoriesModel returns a model for the database table.
func NewAiUserMemoriesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiUserMemoriesModel {
	return &customAiUserMemoriesModel{
		defaultAiUserMemoriesModel: newAiUserMemoriesModel(conn, c, opts...),
	}
}
