package eino

import (
	"encoding/json"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const (
	a2uiVersion        = "a2ui.v1"
	a2uiSurfaceUpdate  = "surface_update"
	a2uiDataModelPatch = "data_model_update"
	a2uiInterrupt      = "interrupt_request"
)

type a2uiPayload struct {
	Version        string `json:"version"`
	Type           string `json:"type"`
	SurfaceID      string `json:"surface_id,omitempty"`
	ComponentID    string `json:"component_id,omitempty"`
	Component      string `json:"component,omitempty"`
	DataKey        string `json:"data_key,omitempty"`
	Mode           string `json:"mode,omitempty"`
	Content        string `json:"content,omitempty"`
	Tool           string `json:"tool,omitempty"`
	Status         string `json:"status,omitempty"`
	DataJSON       string `json:"data_json,omitempty"`
	ConfirmationID string `json:"confirmation_id,omitempty"`
	ExpiresAt      int64  `json:"expires_at,omitempty"`
	Done           bool   `json:"done,omitempty"`
}

func a2uiEvent(conversationID, messageID string, payload a2uiPayload) domain.AgentEvent {
	payload.Version = a2uiVersion
	payload.SurfaceID = conversationID
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"version":"a2ui.v1","type":"error","content":"a2ui encode failed"}`)
	}
	return domain.AgentEvent{
		Type:           domain.EventA2UI,
		ConversationID: conversationID,
		MessageID:      messageID,
		DataJSON:       string(raw),
		Done:           payload.Done,
	}
}

func a2uiDataKey(component, messageID string) string {
	return component + ":" + messageID
}

func a2uiThinkingDelta(conversationID, messageID, content string) domain.AgentEvent {
	return a2uiEvent(conversationID, messageID, a2uiPayload{
		Type:        a2uiDataModelPatch,
		ComponentID: messageID,
		Component:   "thinking",
		DataKey:     a2uiDataKey("thinking", messageID),
		Mode:        "append",
		Content:     content,
	})
}

func a2uiAssistantDelta(conversationID, messageID, content string) domain.AgentEvent {
	return a2uiEvent(conversationID, messageID, a2uiPayload{
		Type:        a2uiDataModelPatch,
		ComponentID: messageID,
		Component:   "assistant_answer",
		DataKey:     a2uiDataKey("assistant_answer", messageID),
		Mode:        "append",
		Content:     content,
	})
}

func a2uiAssistantDone(conversationID, messageID string) domain.AgentEvent {
	return a2uiEvent(conversationID, messageID, a2uiPayload{
		Type:        a2uiDataModelPatch,
		ComponentID: messageID,
		Component:   "assistant_answer",
		DataKey:     a2uiDataKey("assistant_answer", messageID),
		Mode:        "done",
		Done:        true,
	})
}

func a2uiToolUpdate(conversationID, messageID, toolName, status, content, dataJSON string, done bool) domain.AgentEvent {
	return a2uiEvent(conversationID, messageID, a2uiPayload{
		Type:        a2uiSurfaceUpdate,
		ComponentID: messageID,
		Component:   "tool_call",
		DataKey:     a2uiDataKey("tool_call", messageID),
		Mode:        "replace",
		Tool:        toolName,
		Status:      status,
		Content:     content,
		DataJSON:    dataJSON,
		Done:        done,
	})
}

func a2uiConfirmation(conversationID, messageID, confirmationID, action, summary, dataJSON string, expiresAt int64) domain.AgentEvent {
	return a2uiEvent(conversationID, messageID, a2uiPayload{
		Type:           a2uiInterrupt,
		ComponentID:    messageID,
		Component:      "confirmation",
		DataKey:        a2uiDataKey("confirmation", messageID),
		Mode:           "replace",
		ConfirmationID: confirmationID,
		Tool:           action,
		Status:         "pending",
		Content:        summary,
		DataJSON:       dataJSON,
		ExpiresAt:      expiresAt,
		Done:           true,
	})
}
