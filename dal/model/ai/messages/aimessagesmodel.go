package messages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiMessagesModel = (*customAiMessagesModel)(nil)

var ErrNilBatchMessage = errors.New("ai message batch contains nil message")

type (
	// AiMessagesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiMessagesModel.
	AiMessagesModel interface {
		aiMessagesModel
		FindRecentByConversationID(ctx context.Context, conversationID string, limit int) ([]*AiMessages, error)
		FindRecentContextMessages(ctx context.Context, userID uint64, conversationID string, limit int) ([]*AiMessages, error)
		CountUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string) (int64, error)
		FindUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string, limit int) ([]*AiMessages, error)
		FindRecentUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string, limit int) ([]*AiMessages, error)
		FindRecentToolMessages(ctx context.Context, userID uint64, conversationID string, limit int) ([]*AiMessages, error)
		FindToolMessageByID(ctx context.Context, userID uint64, conversationID, messageID string) (*AiMessages, error)
		FindMessagesByIDs(ctx context.Context, userID uint64, conversationID string, messageIDs []string) ([]*AiMessages, error)
		FindUserMessageByClientMessageID(ctx context.Context, userID uint64, clientMessageID string) (*AiMessages, error)
		FindAssistantMessagesByClientMessageID(ctx context.Context, userID uint64, conversationID, clientMessageID string) ([]*AiMessages, error)
		InsertBatch(ctx context.Context, messages []*AiMessages) error
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

// Insert inserts a single message without writing generated columns.
func (m *customAiMessagesModel) Insert(ctx context.Context, data *AiMessages) (sql.Result, error) {
	query := "insert into " + m.table + " (`msg_id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`, `client_message_id`) values (?, ?, ?, ?, ?, ?, ?)"
	return m.CachedConn.ExecNoCacheCtx(ctx, query,
		data.MsgId,
		data.ConversationId,
		data.UserId,
		data.Role,
		data.Content,
		data.Metadata,
		data.ClientMessageId,
	)
}

// Update updates a message without writing generated columns.
func (m *customAiMessagesModel) Update(ctx context.Context, data *AiMessages) error {
	query := "update " + m.table + " set `msg_id` = ?, `conversation_id` = ?, `user_id` = ?, `role` = ?, `content` = ?, `metadata` = ?, `client_message_id` = ? where `seq` = ?"
	_, err := m.CachedConn.ExecNoCacheCtx(ctx, query,
		data.MsgId,
		data.ConversationId,
		data.UserId,
		data.Role,
		data.Content,
		data.Metadata,
		data.ClientMessageId,
		data.Seq,
	)
	return err
}

// InsertBatch 批量插入消息记录
func (m *customAiMessagesModel) InsertBatch(ctx context.Context, messages []*AiMessages) error {
	if len(messages) == 0 {
		return nil
	}

	const rowPlaceholder = "(?, ?, ?, ?, ?, ?, ?)"
	placeholders := make([]string, len(messages))
	args := make([]any, 0, len(messages)*7)
	for i, message := range messages {
		if message == nil {
			return ErrNilBatchMessage
		}
		placeholders[i] = rowPlaceholder
		args = append(args,
			message.MsgId,
			message.ConversationId,
			message.UserId,
			message.Role,
			message.Content,
			message.Metadata,
			message.ClientMessageId,
		)
	}

	query := fmt.Sprintf(
		"insert into %s (`msg_id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`, `client_message_id`) values %s",
		m.table,
		strings.Join(placeholders, ", "),
	)
	_, err := m.CachedConn.ExecNoCacheCtx(ctx, query, args...)
	return err
}

func (m *customAiMessagesModel) FindRecentByConversationID(ctx context.Context, conversationID string, limit int) ([]*AiMessages, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `conversation_id` = ? order by `seq` desc limit ?"
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

// FindRecentContextMessages 查询当前用户会话中最近的 user/assistant 原文，不包含工具消息。
func (m *customAiMessagesModel) FindRecentContextMessages(ctx context.Context, userID uint64, conversationID string, limit int) ([]*AiMessages, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?) order by `seq` desc limit ?"
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, userID, conversationID, "user", "assistant", limit); err != nil {
		return nil, err
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, nil
}

// CountUnsummarizedContextMessages 统计摘要水位之后的 user/assistant 原文数量。
func (m *customAiMessagesModel) CountUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string) (int64, error) {
	var count int64
	query := "select count(1) from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?)"
	args := []any{userID, conversationID, "user", "assistant"}
	if afterMessageID != "" {
		query += " and `seq` > (select `seq` from " + m.table + " where `msg_id` = ? and `user_id` = ? and `conversation_id` = ? limit 1)"
		args = append(args, afterMessageID, userID, conversationID)
	} else if afterCreatedAt != "" {
		query += " and `created_at` > ?"
		args = append(args, afterCreatedAt)
	}
	if err := m.CachedConn.QueryRowNoCacheCtx(ctx, &count, query, args...); err != nil {
		return 0, err
	}
	return count, nil
}

// FindUnsummarizedContextMessages 查询摘要水位之后的 user/assistant 原文。
func (m *customAiMessagesModel) FindUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string, limit int) ([]*AiMessages, error) {
	if limit <= 0 {
		limit = 30
	}

	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?)"
	args := []any{userID, conversationID, "user", "assistant"}
	if afterMessageID != "" {
		query += " and `seq` > (select `seq` from " + m.table + " where `msg_id` = ? and `user_id` = ? and `conversation_id` = ? limit 1)"
		args = append(args, afterMessageID, userID, conversationID)
	} else if afterCreatedAt != "" {
		query += " and `created_at` > ?"
		args = append(args, afterCreatedAt)
	}
	// 按创建时间升序排序，确保按时间顺序返回
	query += " order by `seq` asc limit ?"
	args = append(args, limit)
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

// FindRecentUnsummarizedContextMessages 查询摘要水位之后最近的 user/assistant 原文，并按时间正序返回。
func (m *customAiMessagesModel) FindRecentUnsummarizedContextMessages(ctx context.Context, userID uint64, conversationID string, afterCreatedAt string, afterMessageID string, limit int) ([]*AiMessages, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?)"
	args := []any{userID, conversationID, "user", "assistant"}
	if afterMessageID != "" {
		query += " and `seq` > (select `seq` from " + m.table + " where `msg_id` = ? and `user_id` = ? and `conversation_id` = ? limit 1)"
		args = append(args, afterMessageID, userID, conversationID)
	} else if afterCreatedAt != "" {
		query += " and `created_at` > ?"
		args = append(args, afterCreatedAt)
	}
	query += " order by `seq` desc limit ?"
	args = append(args, limit)
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, nil
}

// FindRecentToolMessages 查询最近的工具消息记录
func (m *customAiMessagesModel) FindRecentToolMessages(ctx context.Context, userID uint64, conversationID string, limit int) ([]*AiMessages, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `role` = ? order by `seq` desc limit ?"
	err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, userID, conversationID, "tool", limit)
	return rows, err
}

// FindToolMessageByID 根据ID查询工具消息
func (m *customAiMessagesModel) FindToolMessageByID(ctx context.Context, userID uint64, conversationID, messageID string) (*AiMessages, error) {
	var row AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `msg_id` = ? and `user_id` = ? and `conversation_id` = ? and `role` = ? limit 1"
	if err := m.CachedConn.QueryRowNoCacheCtx(ctx, &row, query, messageID, userID, conversationID, "tool"); err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *customAiMessagesModel) FindMessagesByIDs(ctx context.Context, userID uint64, conversationID string, messageIDs []string) ([]*AiMessages, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, 0, len(messageIDs)+2)
	args = append(args, userID, conversationID)
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `msg_id` in (" + strings.Join(placeholders, ",") + ") order by `seq` asc"
	var rows []*AiMessages
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *customAiMessagesModel) FindUserMessageByClientMessageID(ctx context.Context, userID uint64, clientMessageID string) (*AiMessages, error) {
	var row AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `client_message_id` = ? and `role` = ? limit 1"
	if err := m.CachedConn.QueryRowNoCacheCtx(ctx, &row, query, userID, clientMessageID, "user"); err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *customAiMessagesModel) FindAssistantMessagesByClientMessageID(ctx context.Context, userID uint64, conversationID, clientMessageID string) ([]*AiMessages, error) {
	var rows []*AiMessages
	query := "select " + aiMessagesRows + " from " + m.table + " where `user_id` = ? and `conversation_id` = ? and `client_message_id` = ? and `role` = ? order by `seq` asc"
	if err := m.CachedConn.QueryRowsNoCacheCtx(ctx, &rows, query, userID, conversationID, clientMessageID, "assistant"); err != nil {
		return nil, err
	}
	return rows, nil
}
