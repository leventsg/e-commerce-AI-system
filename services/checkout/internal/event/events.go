package event

type CancelOrder struct {
	PreOrderId string `json:"pre_order_id"`
	UserId     int32  `json:"user_id"`
}
