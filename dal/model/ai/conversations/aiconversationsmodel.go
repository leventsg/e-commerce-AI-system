package conversations

import (
	"context"
	"database/sql"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiConversationsModel = (*customAiConversationsModel)(nil)

type (
	// AiConversationsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiConversationsModel.
	AiConversationsModel interface {
		aiConversationsModel
		FindByUserWithStats(ctx context.Context, userID uint64, limit, offset int) ([]*ConversationListItem, error)
		CountByUser(ctx context.Context, userID uint64) (int64, error)
	}

	// ConversationListItem 会话列表项，附带最后一条消息预览、最后活跃时间和消息数。
	ConversationListItem struct {
		Id                 string       `db:"id"`
		UserId             uint64       `db:"user_id"`
		Title              string       `db:"title"`
		Status             string       `db:"status"`
		CreatedAt          time.Time    `db:"created_at"`
		UpdatedAt          time.Time    `db:"updated_at"`
		LastMessageAt      sql.NullTime `db:"last_message_at"`
		LastMessagePreview string       `db:"last_message_preview"`
		MessageCount       int64        `db:"message_count"`
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

// FindByUserWithStats 分页查询当前用户的会话，按最后活跃时间倒序排列。
// 最后活跃时间取会话内最新消息 created_at，无消息时回退到会话 created_at。
func (m *customAiConversationsModel) FindByUserWithStats(ctx context.Context, userID uint64, limit, offset int) ([]*ConversationListItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var rows []*ConversationListItem
	query := "select c.id, c.user_id, c.title, c.status, c.created_at, c.updated_at, " +
		"coalesce((select max(m.created_at) from `ai_messages` m where m.conversation_id = c.id and m.user_id = ?), c.created_at) as last_message_at, " +
		"coalesce((select m.content from `ai_messages` m where m.conversation_id = c.id and m.user_id = ? and m.role in (?, ?) order by m.id desc limit 1), '') as last_message_preview, " +
		"(select count(1) from `ai_messages` m where m.conversation_id = c.id and m.user_id = ?) as message_count " +
		"from " + m.table + " c where c.user_id = ? " +
		"order by last_message_at desc, c.created_at desc limit ? offset ?"
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, userID, userID, "user", "assistant", userID, userID, limit, offset); err != nil {
		return nil, err
	}
	return rows, nil
}

// CountByUser 统计当前用户的会话总数。
func (m *customAiConversationsModel) CountByUser(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	query := "select count(1) from " + m.table + " where `user_id` = ?"
	if err := m.CachedConn.QueryRowNoCacheCtx(ctx, &count, query, userID); err != nil {
		return 0, err
	}
	return count, nil
}
