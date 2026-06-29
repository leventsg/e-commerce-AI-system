package payment_success

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

type InventoryDecreaser interface {
	DecreaseInventory(ctx context.Context, in *inventory.InventoryReq) (*inventory.InventoryResp, error)
}

type PaymentSuccessConsumer struct {
	inventoryRpc InventoryDecreaser
}

func NewPaymentSuccessConsumer(inventoryRpc InventoryDecreaser) *PaymentSuccessConsumer {
	return &PaymentSuccessConsumer{inventoryRpc: inventoryRpc}
}

func (c *PaymentSuccessConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.PaymentSuccess{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if data.PreOrderId == "" || data.UserId == 0 || len(data.Items) == 0 {
		logx.Errorw("inventory payment success event missing required fields",
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("items", data.Items))
		return errors.New("missing required payment success fields")
	}
	if c.inventoryRpc == nil {
		return errors.New("InventoryRpc is nil")
	}

	resp, err := c.inventoryRpc.DecreaseInventory(ctx, &inventory.InventoryReq{
		PreOrderId: data.PreOrderId,
		UserId:     data.UserId,
		Items:      paymentSuccessItemsToInventoryItems(data.Items),
	})
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		return fmt.Errorf("inventory payment success decrease inventory failed: %s", resp.StatusMsg)
	}
	return nil
}

func paymentSuccessItemsToInventoryItems(items []*event.PaymentSuccessItem) []*inventory.InventoryReq_Items {
	res := make([]*inventory.InventoryReq_Items, 0, len(items))
	for _, item := range items {
		res = append(res, &inventory.InventoryReq_Items{
			ProductId: item.ProductId,
			Quantity:  item.Quantity,
		})
	}
	return res
}
