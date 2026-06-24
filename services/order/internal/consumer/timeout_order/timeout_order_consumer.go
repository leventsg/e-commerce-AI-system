package timeout_order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/event"
	service_order "github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type TimeoutOrderConsumer struct {
	Config          config.Config
	OrdersModel     order.OrdersModel
	OrderItemsModel order.OrderItemsModel
	OutboxModel     order.OutboxMessagesModel
	Model           sqlx.SqlConn
}

func NewTimeoutOrderConsumer(
	c config.Config,
	ordersModel order.OrdersModel,
	orderItemsModel order.OrderItemsModel,
	outboxModel order.OutboxMessagesModel,
	model sqlx.SqlConn,
) *TimeoutOrderConsumer {
	return &TimeoutOrderConsumer{
		Config:          c,
		OrdersModel:     ordersModel,
		OrderItemsModel: orderItemsModel,
		OutboxModel:     outboxModel,
		Model:           model,
	}
}

func (co *TimeoutOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.TimeoutOrder{}
	var orderRes *order.Orders
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if err := co.Model.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		oRes, err := co.OrdersModel.WithSession(session).GetOrderByOrderIDAndUserIDWithLock(ctx, data.OrderId, data.UserId)
		if err != nil {
			if errors.Is(err, sqlx.ErrNotFound) {
				logx.Errorw("timeout order is not exist", logx.Field("order_id", data.OrderId), logx.Field("user_id", data.UserId))
				return nil
			}
			return err
		}

		orderRes = oRes
		orderStatus := service_order.OrderStatus(oRes.OrderStatus)
		paymentStatus := service_order.PaymentStatus(oRes.PaymentStatus)

		// 允许继续退预扣库存，方便失败重试
		if orderStatus == service_order.OrderStatus_ORDER_STATUS_CLOSED &&
			paymentStatus == service_order.PaymentStatus_PAYMENT_STATUS_EXPIRED {
			logx.Infow("timeout order already processed, continue returning pre inventory",
				logx.Field("order_id", data.OrderId),
				logx.Field("user_id", data.UserId))
			return nil
		}
		if !shouldCloseTimeoutOrder(data.Source, orderStatus, paymentStatus) {
			logx.Infow("timeout order status skipped",
				logx.Field("order_id", data.OrderId),
				logx.Field("user_id", data.UserId),
				logx.Field("source", data.Source),
				logx.Field("order_status", oRes.OrderStatus),
				logx.Field("payment_status", oRes.PaymentStatus))
			return nil
		}

		if err := co.OrdersModel.WithSession(session).UpdateOrderStatusByOrderIDAndUserID(
			ctx,
			data.OrderId,
			data.UserId,
			service_order.OrderStatus_ORDER_STATUS_CLOSED,
			service_order.PaymentStatus_PAYMENT_STATUS_EXPIRED,
		); err != nil {
			return err
		}
		return nil
	}); err != nil {
		logx.Errorw("close timeout order failed", logx.Field("err", err), logx.Field("order_id", data.OrderId), logx.Field("user_id", data.UserId))
		return err
	}

	return co.saveFullTimeoutOutbox(ctx, orderRes, data.Source)
}

func (co *TimeoutOrderConsumer) saveFullTimeoutOutbox(ctx context.Context, orderRes *order.Orders, source string) error {
	kafkaConfig, err := co.Config.KafkaMQ.TopicConfig("TimeoutOrders")
	if err != nil {
		return err
	}
	orderItems, err := co.OrderItemsModel.QueryOrderItemsByOrderID(ctx, orderRes.OrderId)
	if err != nil {
		logx.Errorw("query timeout order items failed", logx.Field("err", err), logx.Field("order_id", orderRes.OrderId), logx.Field("user_id", orderRes.UserId))
		return err
	}
	if len(orderItems) == 0 {
		return fmt.Errorf("timeout order outbox missing order items: order_id=%s user_id=%d", orderRes.OrderId, orderRes.UserId)
	}

	payload, err := json.Marshal(&event.TimeoutOrder{
		OrderId:    orderRes.OrderId,
		UserId:     int32(orderRes.UserId),
		Source:     source,
		PreOrderId: orderRes.PreOrderId,
		CouponId:   orderRes.CouponId,
		Items:      convertToInventoryItems(orderItems),
	})
	if err != nil {
		return err
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		messageID = uuid.New()
	}
	maxRetry := co.Config.Outbox.MaxRetry
	if maxRetry <= 0 {
		maxRetry = biz.DefaultOutboxMaxRetry
	}
	_, err = co.OutboxModel.Insert(ctx, &order.OutboxMessages{
		MessageId:     messageID.String(),
		EventType:     timeoutEventType(source),
		Topic:         kafkaConfig.Topic,
		MessageKey:    orderRes.OrderId,
		Payload:       string(payload),
		Status:        order.OutboxStatusPending,
		RetryCount:    0,
		MaxRetryCount: int64(maxRetry),
		NextRetryAt:   time.Now(),
		LockedUntil:   sql.NullTime{},
		LastError:     sql.NullString{},
		SentAt:        sql.NullTime{},
	})
	return err
}

func shouldCloseTimeoutOrder(source string, orderStatus service_order.OrderStatus, paymentStatus service_order.PaymentStatus) bool {
	if source == "" {
		source = biz.TimeoutSourceOrder
	}
	switch source {
	case biz.TimeoutSourceOrder:
		// 订单超时
		return orderStatus == service_order.OrderStatus_ORDER_STATUS_CREATED &&
			paymentStatus == service_order.PaymentStatus_PAYMENT_STATUS_NOT_PAID
	case biz.TimeoutSourcePaymentTimeout, biz.TimeoutSourcePaymentFailed:
		// 支付超时或失败
		return orderStatus == service_order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT &&
			paymentStatus == service_order.PaymentStatus_PAYMENT_STATUS_PAYING
	default:
		return false
	}
}

func timeoutEventType(source string) string {
	if source == biz.TimeoutSourcePaymentFailed {
		return biz.PaymentFailedEventType
	}
	if source == biz.TimeoutSourcePaymentTimeout {
		return biz.PaymentTimeoutEventType
	}
	return biz.TimeoutOrderEventType
}

func convertToInventoryItems(orderItems []*order.OrderItems) []*inventory.InventoryReq_Items {
	inventoryItems := make([]*inventory.InventoryReq_Items, len(orderItems))
	for i, item := range orderItems {
		inventoryItems[i] = &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		}
	}
	return inventoryItems
}
