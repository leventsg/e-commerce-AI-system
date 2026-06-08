package event

type CancelOrder struct {
	OrderID string `json:"order_id"`
	UserID  int32  `json:"user_id"`
	Reason  string `json:"reason"`
}
