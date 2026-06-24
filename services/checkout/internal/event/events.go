package event

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
