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
