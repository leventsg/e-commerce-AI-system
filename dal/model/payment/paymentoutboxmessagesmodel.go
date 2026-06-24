package payment

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	commonoutbox "github.com/leventsg/e-commerce-AI-system/common/outbox"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	PaymentOutboxStatusPending int64 = iota
	PaymentOutboxStatusSending
	PaymentOutboxStatusSent
	PaymentOutboxStatusFailed
)

var _ PaymentOutboxMessagesModel = (*customPaymentOutboxMessagesModel)(nil)

type (
	PaymentOutboxMessagesModel interface {
		paymentOutboxMessagesModel
		WithSession(session sqlx.Session) PaymentOutboxMessagesModel
		ClaimPending(ctx context.Context, limit int, lockTTL time.Duration) ([]*commonoutbox.Message, error)
		MarkSent(ctx context.Context, id uint64) error
		MarkRetry(ctx context.Context, id uint64, lastError string, nextRetryAt time.Time) error
		MarkFailed(ctx context.Context, id uint64, lastError string) error
	}

	customPaymentOutboxMessagesModel struct {
		*defaultPaymentOutboxMessagesModel
	}
)

func NewPaymentOutboxMessagesModel(conn sqlx.SqlConn) PaymentOutboxMessagesModel {
	return &customPaymentOutboxMessagesModel{
		defaultPaymentOutboxMessagesModel: newPaymentOutboxMessagesModel(conn),
	}
}

func (m *customPaymentOutboxMessagesModel) WithSession(session sqlx.Session) PaymentOutboxMessagesModel {
	return NewPaymentOutboxMessagesModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customPaymentOutboxMessagesModel) Insert(ctx context.Context, data *PaymentOutboxMessages) (sql.Result, error) {
	query := fmt.Sprintf(`insert into %s (%s) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on duplicate key update
	status = if(status = ?, status, values(status)),
	payload = if(status = ?, values(payload), payload),
	next_retry_at = if(status = ?, values(next_retry_at), next_retry_at),
	updated_at = current_timestamp`, m.table, paymentOutboxMessagesRowsExpectAutoSet)
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
		PaymentOutboxStatusSent,
		PaymentOutboxStatusSent,
		PaymentOutboxStatusSent,
	)
}

func (m *customPaymentOutboxMessagesModel) ClaimPending(ctx context.Context, limit int, lockTTL time.Duration) ([]*commonoutbox.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	lockUntil := time.Now().Add(lockTTL)
	var records []*PaymentOutboxMessages
	err := m.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		query := fmt.Sprintf(`select %s from %s
where status in (?, ?)
  and next_retry_at <= current_timestamp
  and (locked_until is null or locked_until <= current_timestamp)
order by id asc
limit ?
for update skip locked`, paymentOutboxMessagesRows, m.table)
		if err := session.QueryRowsCtx(ctx, &records, query, PaymentOutboxStatusPending, PaymentOutboxStatusSending, limit); err != nil {
			if err == sqlx.ErrNotFound {
				return nil
			}
			return err
		}
		updateQuery := fmt.Sprintf("update %s set status = ?, locked_until = ?, updated_at = current_timestamp where id = ?", m.table)
		for _, msg := range records {
			if _, err := session.ExecCtx(ctx, updateQuery, PaymentOutboxStatusSending, lockUntil, msg.Id); err != nil {
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

func (m *customPaymentOutboxMessagesModel) MarkSent(ctx context.Context, id uint64) error {
	query := fmt.Sprintf("update %s set status = ?, sent_at = current_timestamp, locked_until = null, last_error = null where id = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, PaymentOutboxStatusSent, id)
	return err
}

func (m *customPaymentOutboxMessagesModel) MarkRetry(ctx context.Context, id uint64, lastError string, nextRetryAt time.Time) error {
	query := fmt.Sprintf("update %s set status = ?, retry_count = retry_count + 1, next_retry_at = ?, locked_until = null, last_error = ? where id = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, PaymentOutboxStatusPending, nextRetryAt, truncatePaymentOutboxError(lastError), id)
	return err
}

func (m *customPaymentOutboxMessagesModel) MarkFailed(ctx context.Context, id uint64, lastError string) error {
	query := fmt.Sprintf("update %s set status = ?, retry_count = retry_count + 1, locked_until = null, last_error = ? where id = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, PaymentOutboxStatusFailed, truncatePaymentOutboxError(lastError), id)
	return err
}

func truncatePaymentOutboxError(msg string) string {
	const maxLen = 1024
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen]
}
