package checkout_timeout

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

type InventoryReturnPrer interface {
	ReturnPreInventory(ctx context.Context, in *inventory.InventoryReq) (*inventory.InventoryResp, error)
}

type CheckoutTimeoutConsumer struct {
	inventoryRpc InventoryReturnPrer
}

func NewCheckoutTimeoutConsumer(inventoryRpc InventoryReturnPrer) *CheckoutTimeoutConsumer {
	return &CheckoutTimeoutConsumer{inventoryRpc: inventoryRpc}
}

func (c *CheckoutTimeoutConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.CheckoutTimeout{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if data.PreOrderId == "" || data.UserId == 0 || len(data.Items) == 0 {
		logx.Errorw("inventory checkout timeout event missing required fields",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("items", data.Items),
			logx.Field("source", data.Source))
		return errors.New("missing required checkout timeout fields")
	}
	if c.inventoryRpc == nil {
		return errors.New("InventoryRpc is nil")
	}

	resp, err := c.inventoryRpc.ReturnPreInventory(ctx, &inventory.InventoryReq{
		PreOrderId: data.PreOrderId,
		UserId:     data.UserId,
		Items:      data.Items,
	})
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		return fmt.Errorf("inventory checkout timeout return pre inventory failed: %s", resp.StatusMsg)
	}
	return nil
}
