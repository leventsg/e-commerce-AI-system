package timeout_order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/event"
	service_order "github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type TimeoutOrderConsumer struct {
	OrdersModel     order.OrdersModel
	OrderItemsModel order.OrderItemsModel
	InventoryRpc    inventory.InventoryClient
	Model           sqlx.SqlConn
}

func NewTimeoutOrderConsumer(
	ordersModel order.OrdersModel,
	orderItemsModel order.OrderItemsModel,
	inventoryRpc inventory.InventoryClient,
	model sqlx.SqlConn,
) *TimeoutOrderConsumer {
	return &TimeoutOrderConsumer{
		OrdersModel:     ordersModel,
		OrderItemsModel: orderItemsModel,
		InventoryRpc:    inventoryRpc,
		Model:           model,
	}
}

func (co *TimeoutOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.TimeoutOrder{}
	var orderRes *order.Orders
	shouldReturnInventory := true
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
		if orderStatus != service_order.OrderStatus_ORDER_STATUS_CREATED {
			shouldReturnInventory = false
			logx.Infow("timeout order status skipped",
				logx.Field("order_id", data.OrderId),
				logx.Field("user_id", data.UserId),
				logx.Field("order_status", oRes.OrderStatus))
			return nil
		}

		return co.OrdersModel.WithSession(session).UpdateOrderStatusByOrderIDAndUserID(
			ctx,
			data.OrderId,
			data.UserId,
			service_order.OrderStatus_ORDER_STATUS_CLOSED,
			service_order.PaymentStatus_PAYMENT_STATUS_EXPIRED,
		)
	}); err != nil {
		logx.Errorw("close timeout order failed", logx.Field("err", err), logx.Field("order_id", data.OrderId), logx.Field("user_id", data.UserId))
		return err
	}

	if !shouldReturnInventory || orderRes == nil {
		return nil
	}

	orderItems, err := co.OrderItemsModel.QueryOrderItemsByOrderID(ctx, orderRes.OrderId)
	if err != nil {
		logx.Errorw("query timeout order items failed", logx.Field("err", err), logx.Field("order_id", data.OrderId), logx.Field("user_id", data.UserId))
		return err
	}

	inventoryItems := make([]*inventory.InventoryReq_Items, len(orderItems))
	for i, item := range orderItems {
		inventoryItems[i] = &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		}
	}

	resp, err := co.InventoryRpc.ReturnPreInventory(ctx, &inventory.InventoryReq{
		PreOrderId: orderRes.PreOrderId,
		UserId:     int32(orderRes.UserId),
		Items:      inventoryItems,
	})
	if err != nil {
		logx.Errorw("return timeout order pre inventory failed", logx.Field("err", err), logx.Field("order_id", data.OrderId), logx.Field("user_id", data.UserId))
		return err
	}
	if resp.StatusCode != code.Success {
		logx.Errorw("return timeout order pre inventory failed with status",
			logx.Field("order_id", data.OrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("status_code", resp.StatusCode),
			logx.Field("status_msg", resp.StatusMsg))
		return fmt.Errorf("return timeout order pre inventory failed: status_code=%d status_msg=%s", resp.StatusCode, resp.StatusMsg)
	}
	return nil
}
