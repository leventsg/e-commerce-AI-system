package event

type CancelOrder struct {
	OrderId    string `json:"order_id"`
	UserId     int32  `json:"user_id"`
	Reason     string `json:"reason,omitempty"`
	PreOrderId string `json:"pre_order_id"`
	CouponId   string `json:"coupon_id"`
}

type TimeoutOrder struct {
	OrderId    string `json:"order_id"`
	UserId     int32  `json:"user_id"`
	Reason     string `json:"reason,omitempty"`
	PreOrderId string `json:"pre_order_id"`
	CouponId   string `json:"coupon_id"`
}

type PaymentSuccess struct {
	OrderId        string `json:"order_id"`
	PreOrderId     string `json:"pre_order_id"`
	UserId         int32  `json:"user_id"`
	TransactionId  string `json:"transaction_id"`
	PaidAmount     int64  `json:"paid_amount"`
	PaidAt         int64  `json:"paid_at"`
	OriginalAmount int64  `json:"original_amount"`
	DiscountAmount int64  `json:"discount_amount"`
	CouponId       string `json:"coupon_id,omitempty"`
}
