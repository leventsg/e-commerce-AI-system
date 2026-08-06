package domain

const (
	EventAssistantMessage     = "assistant_message"
	EventAssistantDelta       = "assistant_delta"
	EventToolResult           = "tool_result"
	EventToolProgress         = "tool_progress"
	EventConfirmationRequired = "confirmation_required"
	EventError                = "error"
)

type AgentEvent struct {
	Type             string
	ConversationID   string
	MessageID        string
	ToolCallID       string
	Content          string
	Tool             string
	Status           string
	DataJSON         string
	ConfirmationID   string
	Action           string
	Summary          string
	ExpiresAt        int64
	Done             bool
	BusinessExecuted bool
}
