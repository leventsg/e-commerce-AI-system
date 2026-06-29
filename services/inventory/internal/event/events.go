package event

import "github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"

type CancelOrder struct {
	PreOrderId string                          `json:"pre_order_id"`
	UserId     int32                           `json:"user_id"`
	Items      []*inventory.InventoryReq_Items `json:"items"`
}

type TimeoutOrder struct {
	OrderId    string                          `json:"order_id"`
	UserId     int32                           `json:"user_id"`
	Source     string                          `json:"source,omitempty"`
	Reason     string                          `json:"reason,omitempty"`
	PreOrderId string                          `json:"pre_order_id"`
	CouponId   string                          `json:"coupon_id,omitempty"`
	Items      []*inventory.InventoryReq_Items `json:"items"`
}

type CheckoutTimeout struct {
	PreOrderId string                          `json:"pre_order_id"`
	UserId     int32                           `json:"user_id"`
	Source     string                          `json:"source,omitempty"`
	Items      []*inventory.InventoryReq_Items `json:"items"`
}

type PaymentSuccessItem struct {
	ProductId int32 `json:"product_id"`
	Quantity  int32 `json:"quantity"`
}

type PaymentSuccess struct {
	OrderId        string                `json:"order_id"`
	PreOrderId     string                `json:"pre_order_id"`
	UserId         int32                 `json:"user_id"`
	TransactionId  string                `json:"transaction_id"`
	PaidAmount     int64                 `json:"paid_amount"`
	PaidAt         int64                 `json:"paid_at"`
	OriginalAmount int64                 `json:"original_amount"`
	DiscountAmount int64                 `json:"discount_amount"`
	CouponId       string                `json:"coupon_id,omitempty"`
	Items          []*PaymentSuccessItem `json:"items"`
}
