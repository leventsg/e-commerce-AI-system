package timeout

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	paymentmodel "github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/config"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type OrderTimeoutPayload struct {
	OrderId string `json:"order_id"`
	UserId  int32  `json:"user_id"`
	Source  string `json:"source,omitempty"`
}

func SaveOrderTimeoutOutbox(
	ctx context.Context,
	session sqlx.Session,
	c config.Config,
	outbox paymentmodel.PaymentOutboxMessagesModel,
	paymentRes *paymentmodel.Payments,
	eventType string,
	source string,
) error {
	kafkaConfig, err := c.KafkaMQ.TopicConfig("TimeoutOrders")
	if err != nil {
		return err
	}
	payload, err := json.Marshal(&OrderTimeoutPayload{
		OrderId: paymentRes.OrderId.String,
		UserId:  int32(paymentRes.UserId),
		Source:  source,
	})
	if err != nil {
		return err
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		messageID = uuid.New()
	}
	maxRetry := c.Outbox.MaxRetry
	if maxRetry <= 0 {
		maxRetry = biz.DefaultOutboxMaxRetry
	}
	_, err = outbox.WithSession(session).Insert(ctx, &paymentmodel.PaymentOutboxMessages{
		MessageId:     messageID.String(),
		EventType:     eventType,
		Topic:         kafkaConfig.Topic,
		MessageKey:    paymentRes.OrderId.String,
		Payload:       string(payload),
		Status:        paymentmodel.PaymentOutboxStatusPending,
		RetryCount:    0,
		MaxRetryCount: int64(maxRetry),
		NextRetryAt:   time.Now(),
		LockedUntil:   sql.NullTime{},
		LastError:     sql.NullString{},
		SentAt:        sql.NullTime{},
	})
	return err
}
