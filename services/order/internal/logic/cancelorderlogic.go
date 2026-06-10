package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	order2 "github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/event"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/zeromicro/go-zero/core/logx"
)

type CancelOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCancelOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelOrderLogic {
	return &CancelOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CancelOrder 取消订单 由用户发起
func (l *CancelOrderLogic) CancelOrder(in *order.CancelOrderRequest) (*order.EmptyRes, error) {
	res := &order.EmptyRes{}
	if err := l.svcCtx.Model.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
		oRes, err := l.svcCtx.OrderModel.WithSession(session).GetOrderByOrderIDAndUserIDWithLock(ctx, in.OrderId, int32(in.UserId))
		if err != nil {
			if errors.Is(err, sqlx.ErrNotFound) {
				res.StatusCode = code.OrderNotExist
				res.StatusMsg = code.OrderNotExistMsg
				return nil
			}
			return err
		}
		switch order.OrderStatus(oRes.OrderStatus) {
		case order.OrderStatus_ORDER_STATUS_CREATED, order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT:
			//	可以取消
			if err := l.svcCtx.OrderModel.WithSession(session).CancelOrder(ctx, int32(in.UserId), in.OrderId, in.CancelReason); err != nil {
				return err
			}
			return l.saveCancelOrderOutbox(ctx, session, oRes, in)
		case order.OrderStatus_ORDER_STATUS_PAID:
			res.StatusCode = code.OrderAlreadyPaid
			res.StatusMsg = code.OrderAlreadyPaidMsg
			return nil
		case order.OrderStatus_ORDER_STATUS_COMPLETED:
			res.StatusCode = code.OrderAlreadyCompleted
			res.StatusMsg = code.OrderAlreadyCompletedMsg
			return nil
		case order.OrderStatus_ORDER_STATUS_CANCELLED:
			res.StatusCode = code.OrderAlreadyCancelled
			res.StatusMsg = code.OrderAlreadyCancelledMsg
			return nil
		case order.OrderStatus_ORDER_STATUS_CLOSED:
			res.StatusCode = code.OrderAlreadyClosed
			res.StatusMsg = code.OrderAlreadyClosedMsg
			return nil
		case order.OrderStatus_ORDER_STATUS_REFUND:
			res.StatusCode = code.OrderAlreadyRefund
			res.StatusMsg = code.OrderAlreadyRefundMsg
			return nil

		}
		return nil
	}); err != nil {
		l.Logger.Errorw("cancel order failed", logx.Field("err", err),
			logx.Field("order_id", in.OrderId), logx.Field("user_id", in.UserId))
		return nil, err
	}
	return res, nil
}

func (l *CancelOrderLogic) saveCancelOrderOutbox(ctx context.Context, session sqlx.Session, orderRes *order2.Orders, in *order.CancelOrderRequest) error {
	kafkaConfig, err := l.svcCtx.Config.KafkaMQ.TopicConfig("CancelOrders")
	if err != nil {
		l.Logger.Errorw("get cancel-orders kafka config failed", logx.Field("err", err))
		return err
	}

	orderItems, err := l.svcCtx.OrderItemModel.WithSession(session).QueryOrderItemsByOrderID(ctx, orderRes.OrderId)
	if err != nil {
		l.Logger.Errorw("query order items failed", logx.Field("err", err))
		return err
	}
	if len(orderItems) == 0 {
		return fmt.Errorf("cancel order outbox missing order items: order_id=%s user_id=%d", in.OrderId, in.UserId)
	}
	inventoryItems := make([]*inventory.InventoryReq_Items, len(orderItems))
	for i, item := range orderItems {
		inventoryItems[i] = &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		}
	}

	// 构建取消订单事件
	event := &event.CancelOrder{
		OrderId:    in.OrderId,
		UserId:     int32(in.UserId),
		Reason:     in.CancelReason,
		PreOrderId: orderRes.PreOrderId,
		CouponId:   orderRes.CouponId,
		Items:      inventoryItems,
	}
	msg, err := json.Marshal(event)
	if err != nil {
		l.Logger.Errorw("json failed", logx.Field("err", err), logx.Field("order_id", in.OrderId), logx.Field("user_id", in.UserId))
		return err
	}

	messageID, err := uuid.NewV7()
	if err != nil {
		l.Logger.Errorw("generate outbox message id failed", logx.Field("err", err), logx.Field("order_id", in.OrderId), logx.Field("user_id", in.UserId))
		return err
	}

	maxRetry := l.svcCtx.Config.Outbox.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 10
	}
	_, err = l.svcCtx.OutboxModel.WithSession(session).Insert(ctx, &order2.OutboxMessages{
		MessageId:     messageID.String(),
		EventType:     "order.cancelled",
		Topic:         kafkaConfig.Topic,
		MessageKey:    in.OrderId,
		Payload:       string(msg),
		Status:        order2.OutboxStatusPending,
		RetryCount:    0,
		MaxRetryCount: int64(maxRetry),
		NextRetryAt:   time.Now(),
		LockedUntil:   sql.NullTime{},
		LastError:     sql.NullString{},
		SentAt:        sql.NullTime{},
	})
	return err
}
