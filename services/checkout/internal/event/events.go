package event

import "github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"

type CancelOrder struct {
	PreOrderId string `json:"pre_order_id"`
	UserId     int32  `json:"user_id"`
}

type TimeoutOrder struct {
	OrderId    string `json:"order_id"`
	UserId     int32  `json:"user_id"`
	Source     string `json:"source,omitempty"`
	Reason     string `json:"reason,omitempty"`
	PreOrderId string `json:"pre_order_id"`
	CouponId   string `json:"coupon_id,omitempty"`
}

type CheckoutTimeout struct {
	PreOrderId string                          `json:"pre_order_id"`
	UserId     int32                           `json:"user_id"`
	Source     string                          `json:"source,omitempty"`
	Items      []*inventory.InventoryReq_Items `json:"items"`
}
