package event

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	paymentmodel "github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/config"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const PaymentSuccessesTopicKey = "PaymentSuccesses"

type PaymentSuccessItem struct {
	ProductId uint64 `json:"product_id"`
	Quantity  uint64 `json:"quantity"`
}

type PaymentSuccess struct {
	OrderId        string               `json:"order_id"`
	PreOrderId     string               `json:"pre_order_id"`
	UserId         uint32               `json:"user_id"`
	TransactionId  string               `json:"transaction_id"`
	PaidAmount     int64                `json:"paid_amount"`
	PaidAt         int64                `json:"paid_at"`
	OriginalAmount int64                `json:"original_amount"`
	DiscountAmount int64                `json:"discount_amount"`
	CouponId       string               `json:"coupon_id,omitempty"`
	Items          []PaymentSuccessItem `json:"items"`
}

func NewPaymentSuccessOutboxMessage(
	ctx context.Context,
	c config.Config,
	orderDetail *order.OrderDetailResponse,
	couponID string,
) (*paymentmodel.PaymentOutboxMessages, error) {
	if orderDetail == nil || orderDetail.Order == nil {
		return nil, errors.New("order detail is nil")
	}
	if orderDetail.Order.OrderId == "" || orderDetail.Order.PreOrderId == "" || orderDetail.Order.UserId == 0 {
		return nil, errors.New("order detail missing required fields")
	}
	if len(orderDetail.Items) == 0 {
		return nil, errors.New("order detail missing items")
	}

	kafkaConfig, err := c.KafkaMQ.TopicConfig(PaymentSuccessesTopicKey)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(BuildPaymentSuccessPayload(orderDetail, couponID))
	if err != nil {
		return nil, err
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		messageID = uuid.New()
	}
	maxRetry := c.Outbox.MaxRetry
	if maxRetry <= 0 {
		maxRetry = biz.DefaultOutboxMaxRetry
	}
	return &paymentmodel.PaymentOutboxMessages{
		MessageId:     messageID.String(),
		EventType:     biz.PaymentSuccessEventType,
		Topic:         kafkaConfig.Topic,
		MessageKey:    orderDetail.Order.OrderId,
		Payload:       string(payload),
		Status:        paymentmodel.PaymentOutboxStatusPending,
		RetryCount:    0,
		MaxRetryCount: int64(maxRetry),
		NextRetryAt:   time.Now(),
		LockedUntil:   sql.NullTime{},
		LastError:     sql.NullString{},
		SentAt:        sql.NullTime{},
	}, nil
}

func BuildPaymentSuccessPayload(orderDetail *order.OrderDetailResponse, couponID string) PaymentSuccess {
	items := make([]PaymentSuccessItem, 0, len(orderDetail.Items))
	for _, item := range orderDetail.Items {
		items = append(items, PaymentSuccessItem{
			ProductId: item.ProductId,
			Quantity:  item.Quantity,
		})
	}
	return PaymentSuccess{
		OrderId:        orderDetail.Order.OrderId,
		PreOrderId:     orderDetail.Order.PreOrderId,
		UserId:         orderDetail.Order.UserId,
		TransactionId:  orderDetail.Order.TransactionId,
		PaidAmount:     orderDetail.Order.PaidAmount,
		PaidAt:         orderDetail.Order.PaidAt,
		OriginalAmount: orderDetail.Order.OriginalAmount,
		DiscountAmount: orderDetail.Order.DiscountAmount,
		CouponId:       couponID,
		Items:          items,
	}
}

func SavePaymentSuccessOutbox(
	ctx context.Context,
	session sqlx.Session,
	c config.Config,
	outbox paymentmodel.PaymentOutboxMessagesModel,
	orderDetail *order.OrderDetailResponse,
	couponID string,
) error {
	if outbox == nil {
		return errors.New("payment outbox model is nil")
	}
	message, err := NewPaymentSuccessOutboxMessage(ctx, c, orderDetail, couponID)
	if err != nil {
		return err
	}
	if session != nil {
		outbox = outbox.WithSession(session)
	}
	_, err = outbox.Insert(ctx, message)
	return err
}
