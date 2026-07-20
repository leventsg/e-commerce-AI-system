package messages

import (
	"context"
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

// InsertBatch 批量插入消息记录
func (m *customAiMessagesModel) InsertBatch(ctx context.Context, messages []*AiMessages) error {
	if len(messages) == 0 {
		return nil
	}

	const rowPlaceholder = "(?, ?, ?, ?, ?, ?)"
	placeholders := make([]string, len(messages))
	args := make([]any, 0, len(messages)*6)
	for i, message := range messages {
		if message == nil {
			return ErrNilBatchMessage
		}
		placeholders[i] = rowPlaceholder
		args = append(args,
			message.Id,
			message.ConversationId,
			message.UserId,
			message.Role,
			message.Content,
			message.Metadata,
		)
	}

	query := fmt.Sprintf(
		"insert into %s (`id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`) values %s",
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
