package event

type AuditLog struct {
	UserID      uint32 `json:"user_id"`
	ActionType  string `json:"action_type"`
	ActionDesc  string `json:"action_desc"`
	TargetTable string `json:"target_table"`
	TargetID    int64  `json:"target_id"`
	OldData     string `json:"old_data"`
	NewData     string `json:"new_data"`
	ServiceName string `json:"service_name"`

	// trace
	TraceID  string `json:"trace_id"`
	SpanID   string `json:"span_id"`
	ClientIP string `json:"client_ip"`

	CreatedAt int64 `json:"created_at"`
}
