package timeout_order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/event"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/zeromicro/go-zero/core/logx"
)

type TimeoutOrderConsumer struct {
	InventoryRpc InventoryReturnPrer
}

func NewTimeoutOrderConsumer(inventoryRpc InventoryReturnPrer) *TimeoutOrderConsumer {
	return &TimeoutOrderConsumer{
		InventoryRpc: inventoryRpc,
	}
}

func (co *TimeoutOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.TimeoutOrder{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}

	if len(data.PreOrderId) == 0 || data.UserId == 0 || len(data.Items) == 0 {
		logx.Errorw("inventory timeout order event missing required fields",
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("items", data.Items),
			logx.Field("source", data.Source))
		return errors.New("missing required timeout order fields")
	}

	if co.InventoryRpc == nil {
		logx.Errorw("inventory timeout order event received but InventoryRpc is nil",
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId))
		return errors.New("InventoryRpc is nil")
	}

	resp, err := co.InventoryRpc.ReturnPreInventory(ctx, &inventory.InventoryReq{
		PreOrderId: data.PreOrderId,
		UserId:     data.UserId,
		Items:      data.Items,
	})
	if err != nil {
		logx.Errorw("failed to return timeout pre inventory",
			logx.Field("err", err),
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId))
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		logx.Errorw("inventory rpc ReturnPreInventory failed with status",
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("status_code", resp.StatusCode),
			logx.Field("status_msg", resp.StatusMsg))
		return fmt.Errorf("inventory timeout_order consumer failed to return pre inventory: %s", resp.StatusMsg)
	}
	return nil
}
