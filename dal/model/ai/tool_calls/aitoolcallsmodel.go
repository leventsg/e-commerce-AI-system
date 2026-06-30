package tool_calls

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiToolCallsModel = (*customAiToolCallsModel)(nil)

type (
	// AiToolCallsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiToolCallsModel.
	AiToolCallsModel interface {
		aiToolCallsModel
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
