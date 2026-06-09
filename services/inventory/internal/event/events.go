package event

import "github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"

type CancelOrder struct {
	PreOrderId string                          `json:"pre_order_id"`
	UserId     int32                           `json:"user_id"`
	Items      []*inventory.InventoryReq_Items `json:"items"`
}
