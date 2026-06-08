package logic

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	order2 "github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
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
	var orderRes *order2.Orders
	if err := l.svcCtx.Model.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
		oRes, err := l.svcCtx.OrderModel.WithSession(session).GetOrderByOrderIDAndUserIDWithLock(ctx, in.OrderId, int32(in.UserId))
		orderRes = oRes
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
			return nil
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
	if res.StatusCode != code.Success {
		return res, nil
	}
	kafkaCongif, err := l.svcCtx.Config.KafkaMQ.TopicConfig("CancelOrders")
	if err != nil {
		l.Logger.Errorw("get cancel-orders kafka config failed", logx.Field("err", err))
		return nil, err
	}
	event := &event.CancelOrder{
		OrderId:    in.OrderId,
		UserId:     int32(in.UserId),
		Reason:     in.CancelReason,
		PreOrderId: orderRes.PreOrderId,
		CouponId:   orderRes.CouponId,
	}
	msg, err := json.Marshal(event)
	if err != nil {
		l.Logger.Errorw("json failed", logx.Field("err", err), logx.Field("order_id", in.OrderId), logx.Field("user_id", in.UserId))
		return nil, err
	}

	// 发mq消息，异步处理后续的退款和库存回滚等操作
	if err := l.svcCtx.Producer.Publish(l.ctx, kafkaCongif.Topic, msg); err != nil {
		l.Logger.Errorw("publish cancel order event failed", logx.Field("err", err), logx.Field("order_id", in.OrderId), logx.Field("user_id", in.UserId))
		return nil, err
	}

	// TODO: 预订单释放和库存回滚后续也用mq消费
	// 释放 checkout 资源，优惠券由 cancel-orders MQ 事件异步释放。
	releaseCheckout, err := l.svcCtx.CheckoutRpc.ReleaseCheckout(l.ctx, &checkout.ReleaseReq{
		PreOrderId: orderRes.PreOrderId,
		UserId:     int32(in.UserId),
	})
	if err != nil {
		l.Logger.Errorw("call rpc ReleaseCheckout failed", logx.Field("err", err))
		return nil, err
	}
	if releaseCheckout.StatusCode != code.Success {
		res.StatusCode = releaseCheckout.StatusCode
		res.StatusMsg = releaseCheckout.StatusMsg
		return res, nil
	}
	orderItems, err := l.svcCtx.OrderItemModel.QueryOrderItemsByOrderID(l.ctx, orderRes.OrderId)
	if err != nil {
		l.Logger.Errorw("query order items failed", logx.Field("err", err))
		return nil, err
	}
	inventoryItems := make([]*inventory.InventoryReq_Items, len(orderItems))
	for i, item := range orderItems {
		inventoryItems[i] = &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		}
	}
	// 退还库存
	inventoryResp, err := l.svcCtx.InventoryRpc.ReturnInventory(l.ctx, &inventory.InventoryReq{
		PreOrderId: orderRes.PreOrderId,
		UserId:     int32(in.UserId),
		Items:      inventoryItems,
	})
	if err != nil {
		l.Logger.Errorw("call rpc ReturnInventory failed", logx.Field("err", err))
		return nil, err
	}
	if inventoryResp.StatusCode != code.Success {
		res.StatusCode = inventoryResp.StatusCode
		res.StatusMsg = inventoryResp.StatusMsg
	}
	return res, nil
}
