package confirmations

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiConfirmationsModel = (*customAiConfirmationsModel)(nil)

type (
	// AiConfirmationsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiConfirmationsModel.
	AiConfirmationsModel interface {
		aiConfirmationsModel
	}

	customAiConfirmationsModel struct {
		*defaultAiConfirmationsModel
	}
)

// NewAiConfirmationsModel returns a model for the database table.
func NewAiConfirmationsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiConfirmationsModel {
	return &customAiConfirmationsModel{
		defaultAiConfirmationsModel: newAiConfirmationsModel(conn, c, opts...),
	}
}
