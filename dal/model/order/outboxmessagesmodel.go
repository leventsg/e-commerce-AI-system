package order

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	commonoutbox "github.com/leventsg/e-commerce-AI-system/common/outbox"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	OutboxStatusPending int64 = iota
	OutboxStatusSending
	OutboxStatusSent
	OutboxStatusFailed
)

var _ OutboxMessagesModel = (*customOutboxMessagesModel)(nil)

type (
	// OutboxMessagesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customOutboxMessagesModel.
	OutboxMessagesModel interface {
		outboxMessagesModel
		WithSession(session sqlx.Session) OutboxMessagesModel
		ClaimPending(ctx context.Context, limit int, lockTTL time.Duration) ([]*commonoutbox.Message, error)
		MarkSent(ctx context.Context, id uint64) error
		MarkRetry(ctx context.Context, id uint64, lastError string, nextRetryAt time.Time) error
		MarkFailed(ctx context.Context, id uint64, lastError string) error
	}

	customOutboxMessagesModel struct {
		*defaultOutboxMessagesModel
	}
)

// NewOutboxMessagesModel returns a model for the database table.
func NewOutboxMessagesModel(conn sqlx.SqlConn) OutboxMessagesModel {
	return &customOutboxMessagesModel{
		defaultOutboxMessagesModel: newOutboxMessagesModel(conn),
	}
}

func (m *customOutboxMessagesModel) WithSession(session sqlx.Session) OutboxMessagesModel {
	return NewOutboxMessagesModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customOutboxMessagesModel) Insert(ctx context.Context, data *OutboxMessages) (sql.Result, error) {
	query := fmt.Sprintf(`insert into %s (%s) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on duplicate key update
	status = if(status = ?, status, values(status)),
	payload = if(status = ?, values(payload), payload),
	next_retry_at = if(status = ?, values(next_retry_at), next_retry_at),
	updated_at = current_timestamp`, m.table, outboxMessagesRowsExpectAutoSet)
	return m.conn.ExecCtx(ctx, query,
		data.MessageId,
		data.EventType,
		data.Topic,
		data.MessageKey,
		data.Payload,
		data.Status,
		data.RetryCount,
		data.MaxRetryCount,
		data.NextRetryAt,
		data.LockedUntil,
		data.LastError,
		data.SentAt,
		OutboxStatusSent,
		OutboxStatusSent,
		OutboxStatusSent,
	)
}

func (m *customOutboxMessagesModel) ClaimPending(ctx context.Context, limit int, lockTTL time.Duration) ([]*commonoutbox.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	lockUntil := time.Now().Add(lockTTL)
	var records []*OutboxMessages
	err := m.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		query := fmt.Sprintf(`select %s from %s
where status in (?, ?)
  and next_retry_at <= current_timestamp
  and (locked_until is null or locked_until <= current_timestamp)
order by id asc
limit ?
for update skip locked`, outboxMessagesRows, m.table)
		if err := session.QueryRowsCtx(ctx, &records, query, OutboxStatusPending, OutboxStatusSending, limit); err != nil {
			if err == sqlx.ErrNotFound {
				return nil
			}
			return err
		}
		updateQuery := fmt.Sprintf("update %s set status = ?, locked_until = ?, updated_at = current_timestamp where id = ?", m.table)
		for _, msg := range records {
			if _, err := session.ExecCtx(ctx, updateQuery, OutboxStatusSending, lockUntil, msg.Id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	messages := make([]*commonoutbox.Message, len(records))
	for i, record := range records {
		messages[i] = &commonoutbox.Message{
			Id:            record.Id,
			Topic:         record.Topic,
			MessageKey:    record.MessageKey,
			Payload:       record.Payload,
			RetryCount:    record.RetryCount,
			MaxRetryCount: record.MaxRetryCount,
		}
	}
	return messages, nil
}

func (m *customOutboxMessagesModel) MarkSent(ctx context.Context, id uint64) error {
	query := fmt.Sprintf("update %s set status = ?, sent_at = current_timestamp, locked_until = null, last_error = null where id = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, OutboxStatusSent, id)
	return err
}

func (m *customOutboxMessagesModel) MarkRetry(ctx context.Context, id uint64, lastError string, nextRetryAt time.Time) error {
	query := fmt.Sprintf("update %s set status = ?, retry_count = retry_count + 1, next_retry_at = ?, locked_until = null, last_error = ? where id = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, OutboxStatusPending, nextRetryAt, truncateOutboxError(lastError), id)
	return err
}

func (m *customOutboxMessagesModel) MarkFailed(ctx context.Context, id uint64, lastError string) error {
	query := fmt.Sprintf("update %s set status = ?, retry_count = retry_count + 1, locked_until = null, last_error = ? where id = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, OutboxStatusFailed, truncateOutboxError(lastError), id)
	return err
}

func truncateOutboxError(msg string) string {
	const maxLen = 1024
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen]
}
