package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	order2 "github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/event"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type HandlePaymentTimeoutOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHandlePaymentTimeoutOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HandlePaymentTimeoutOrderLogic {
	return &HandlePaymentTimeoutOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 处理支付超时订单，关闭订单并写入Outbox消息表
func (l *HandlePaymentTimeoutOrderLogic) HandlePaymentTimeoutOrder(in *order.HandlePaymentTimeoutOrderRequest) (*order.EmptyRes, error) {
	if in.OrderId == "" || in.UserId == 0 || !isPaymentTimeoutSource(in.Source) {
		return nil, status.Error(codes.Aborted, "参数错误")
	}

	res := &order.EmptyRes{StatusCode: code.Success, StatusMsg: code.SuccessMsg}
	var orderRes *order2.Orders
	shouldWriteOutbox := false
	if err := l.svcCtx.Model.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
		oRes, err := l.svcCtx.OrderModel.WithSession(session).GetOrderByOrderIDAndUserIDWithLock(ctx, in.OrderId, in.UserId)
		if err != nil {
			if errors.Is(err, sqlx.ErrNotFound) {
				res.StatusCode = code.OrderNotExist
				res.StatusMsg = code.OrderNotExistMsg
				l.Logger.Infow("payment timeout order not found",
					logx.Field("order_id", in.OrderId),
					logx.Field("user_id", in.UserId))
				return nil
			}
			return err
		}
		orderRes = oRes
		action := paymentTimeoutOrderAction(order.OrderStatus(oRes.OrderStatus), order.PaymentStatus(oRes.PaymentStatus))
		// 如果订单状态为待支付且支付状态为支付中，则关闭订单
		if action.closeOrder {
			if err := l.svcCtx.OrderModel.WithSession(session).UpdateOrderStatusByOrderIDAndUserID(
				ctx,
				in.OrderId,
				in.UserId,
				order.OrderStatus_ORDER_STATUS_CLOSED,
				order.PaymentStatus_PAYMENT_STATUS_EXPIRED,
			); err != nil {
				return err
			}
		}
		shouldWriteOutbox = action.writeOutbox
		if !action.closeOrder && !action.writeOutbox {
			l.Logger.Infow("payment timeout order status skipped",
				logx.Field("order_id", in.OrderId),
				logx.Field("user_id", in.UserId),
				logx.Field("source", in.Source),
				logx.Field("order_status", oRes.OrderStatus),
				logx.Field("payment_status", oRes.PaymentStatus))
		}
		return nil
	}); err != nil {
		l.Logger.Errorw("handle payment timeout order failed",
			logx.Field("err", err),
			logx.Field("order_id", in.OrderId),
			logx.Field("user_id", in.UserId),
			logx.Field("source", in.Source))
		return nil, status.Error(codes.Internal, "处理支付超时订单失败")
	}
	if res.StatusCode == code.OrderNotExist {
		// 订单不存在，则返回成功, 避免调用方一直重试处理
		return &order.EmptyRes{StatusCode: code.Success, StatusMsg: code.SuccessMsg}, nil
	}
	// 如果订单不存在或不需要写入Outbox，则直接返回
	if res.StatusCode != code.Success || !shouldWriteOutbox || orderRes == nil {
		return res, nil
	}
	// 保存支付超时订单Outbox消息
	if err := l.saveFullTimeoutOutbox(l.ctx, orderRes, in.Source); err != nil {
		l.Logger.Errorw("save payment timeout order outbox failed",
			logx.Field("err", err),
			logx.Field("order_id", in.OrderId),
			logx.Field("user_id", in.UserId),
			logx.Field("source", in.Source))
		return nil, status.Error(codes.Internal, "保存支付超时订单消息失败")
	}
	return res, nil
}

type paymentTimeoutOrderDecision struct {
	closeOrder  bool
	writeOutbox bool
}

func paymentTimeoutOrderAction(orderStatus order.OrderStatus, paymentStatus order.PaymentStatus) paymentTimeoutOrderDecision {
	if orderStatus == order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT &&
		paymentStatus == order.PaymentStatus_PAYMENT_STATUS_PAYING {
		return paymentTimeoutOrderDecision{closeOrder: true, writeOutbox: true}
	}
	if orderStatus == order.OrderStatus_ORDER_STATUS_CLOSED &&
		paymentStatus == order.PaymentStatus_PAYMENT_STATUS_EXPIRED {
		return paymentTimeoutOrderDecision{writeOutbox: true}
	}
	return paymentTimeoutOrderDecision{}
}

func isPaymentTimeoutSource(source string) bool {
	return source == biz.TimeoutSourcePaymentTimeout || source == biz.TimeoutSourcePaymentFailed
}

func (l *HandlePaymentTimeoutOrderLogic) saveFullTimeoutOutbox(ctx context.Context, orderRes *order2.Orders, source string) error {
	kafkaConfig, err := l.svcCtx.Config.KafkaMQ.TopicConfig("TimeoutOrders")
	if err != nil {
		return err
	}
	orderItems, err := l.svcCtx.OrderItemModel.QueryOrderItemsByOrderID(ctx, orderRes.OrderId)
	if err != nil {
		return err
	}
	if len(orderItems) == 0 {
		return fmt.Errorf("payment timeout order outbox missing order items: order_id=%s user_id=%d", orderRes.OrderId, orderRes.UserId)
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
	maxRetry := l.svcCtx.Config.Outbox.MaxRetry
	if maxRetry <= 0 {
		maxRetry = biz.DefaultOutboxMaxRetry
	}
	_, err = l.svcCtx.OutboxModel.Insert(ctx, &order2.OutboxMessages{
		MessageId:     messageID.String(),
		EventType:     timeoutEventType(source),
		Topic:         kafkaConfig.Topic,
		MessageKey:    orderRes.OrderId,
		Payload:       string(payload),
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

func timeoutEventType(source string) string {
	if source == biz.TimeoutSourcePaymentFailed {
		return biz.PaymentFailedEventType
	}
	return biz.PaymentTimeoutEventType
}

func convertToInventoryItems(orderItems []*order2.OrderItems) []*inventory.InventoryReq_Items {
	inventoryItems := make([]*inventory.InventoryReq_Items, len(orderItems))
	for i, item := range orderItems {
		inventoryItems[i] = &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		}
	}
	return inventoryItems
}
