package logic

import (
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestEventForwardStateSuppressesFinalAssistantAfterDelta(t *testing.T) {
	state := newEventForwardState()

	if !state.shouldForward(domain.AgentEvent{Type: domain.EventAssistantDelta, Content: "你"}) {
		t.Fatal("assistant_delta should be forwarded")
	}
	if state.shouldForward(domain.AgentEvent{Type: domain.EventAssistantMessage, Content: "你好", Done: true}) {
		t.Fatal("assistant_message should not be forwarded after delta in the same run")
	}
}

func TestEventForwardStateForwardsFinalAssistantWhenNoDelta(t *testing.T) {
	state := newEventForwardState()

	if !state.shouldForward(domain.AgentEvent{Type: domain.EventAssistantMessage, Content: "你好", Done: true}) {
		t.Fatal("assistant_message should be forwarded when no delta was sent")
	}
}

func TestAgentEventsToMessagesSkipsTransientEvents(t *testing.T) {
	messages, err := agentEventsToMessages(1, "client-1", []domain.AgentEvent{
		{Type: domain.EventAssistantDelta, ConversationID: "conv-1", MessageID: "msg-delta", Content: "你"},
		{Type: domain.EventToolProgress, ConversationID: "conv-1", MessageID: "msg-progress", Content: "正在查询商品..."},
		{Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "msg-final", Content: "你好", Done: true},
	})
	if err != nil {
		t.Fatalf("agentEventsToMessages returned error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	if messages[0].MsgId != "msg-final" || messages[0].Content != "你好" {
		t.Fatalf("message = %+v, want only final assistant message", messages[0])
	}
}
