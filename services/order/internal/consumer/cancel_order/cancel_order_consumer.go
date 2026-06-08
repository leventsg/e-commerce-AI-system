package cancel_order

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/event"
	service_order "github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type CancelOrderConsumer struct {
	OrdersModel order.OrdersModel
	Model       sqlx.SqlConn
}

func NewCancelOrderConsumer(ordersModel order.OrdersModel, model sqlx.SqlConn) *CancelOrderConsumer {
	return &CancelOrderConsumer{
		OrdersModel: ordersModel,
		Model:       model,
	}
}

func (co *CancelOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.CancelOrder{}
	isPendingPayment := false
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if err := co.Model.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		oRes, err := co.OrdersModel.WithSession(session).GetOrderByOrderIDAndUserIDWithLock(ctx, data.OrderID, data.UserID)
		if err != nil {
			if errors.Is(err, sqlx.ErrNotFound) {
				// 订单不存在, 说明orderid传入错误，需要报错告警，直接返回nil，丢弃消息
				logx.Errorw("orderid is not exist", logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
				return nil
			}
			return err
		}
		switch service_order.OrderStatus(oRes.OrderStatus) {
		case service_order.OrderStatus_ORDER_STATUS_CREATED, service_order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT:
			//	可以取消
			if service_order.OrderStatus(oRes.OrderStatus) == service_order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT {
				isPendingPayment = true
			}
			if err := co.OrdersModel.WithSession(session).CancelOrder(ctx, data.UserID, data.OrderID, data.Reason); err != nil {
				return err
			}
			return nil
		case service_order.OrderStatus_ORDER_STATUS_PAID:
			logx.Errorw("order already paid, cannot cancel", logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
			return errors.New("order already paid, cannot cancel, need to refund")
		case service_order.OrderStatus_ORDER_STATUS_COMPLETED:
			logx.Errorw("order already completed, cannot cancel", logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
			return errors.New("order already completed, cannot cancel")
		// --------------------------------------已经取消了，幂等消费--------------------------------------
		case service_order.OrderStatus_ORDER_STATUS_CANCELLED:
			logx.Infow("order already cancelled, cannot cancel", logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
			return nil
		case service_order.OrderStatus_ORDER_STATUS_CLOSED:
			logx.Infow("order already closed, cannot cancel", logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
			return nil
		case service_order.OrderStatus_ORDER_STATUS_REFUND:
			logx.Infow("order already refunded, cannot cancel", logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
			return nil

		}
		return nil
	}); err != nil {
		logx.Errorw("cancel order failed", logx.Field("err", err), logx.Field("order_id", data.OrderID), logx.Field("user_id", data.UserID))
		return err
	}
	// 如果是pending payment状态，说明订单已经被用户取消了，但是还未支付成功，此时需要调用支付服务的接口进行幂等的退款操作
	if isPendingPayment {
		// todo 实现支付服务的接口调用，进行退款操作，注意幂等性，避免重复退款
		// 调用支付服务的接口进行退款操作
	}
	return nil
}
