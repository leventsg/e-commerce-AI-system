package eino

import "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"

type ToolCallEvent struct {
	ConversationID string
	MessageID      string
	Tool           string
	Status         string
	DataJSON       string
}

func ToolCallToAgentEvent(event ToolCallEvent) domain.AgentEvent {
	return domain.AgentEvent{
		Type:           domain.EventToolResult,
		ConversationID: event.ConversationID,
		MessageID:      event.MessageID,
		Tool:           event.Tool,
		Status:         event.Status,
		DataJSON:       event.DataJSON,
		Done:           true,
	}
}
