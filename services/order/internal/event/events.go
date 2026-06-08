package event

type CancelOrder struct {
	OrderId    string `json:"order_id"`
	UserId     int32  `json:"user_id"`
	Reason     string `json:"reason"`
	PreOrderId string `json:"pre_order_id"`
	CouponId   string `json:"coupon_id"`
}

type TimeoutOrder struct {
	OrderID string `json:"order_id"`
	UserID  int32  `json:"user_id"`
}
