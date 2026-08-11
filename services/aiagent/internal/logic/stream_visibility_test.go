package logic

import (
	"context"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
)

func TestAgentEventsToMessagesPersistsOnlyDurableEvents(t *testing.T) {
	messages, err := agentEventsToMessages(1, "client-1", []domain.AgentEvent{
		{Type: domain.EventAssistantDelta, ConversationID: "conv-1", MessageID: "msg-delta", Content: "你"},
		{Type: "assistant_thinking_delta", ConversationID: "conv-1", Content: "我先分析一下"},
		{Type: domain.EventToolProgress, ConversationID: "conv-1", MessageID: "msg-progress", Content: "正在查询商品..."},
		{Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "msg-final", Content: "你好", Done: true},
		{Type: domain.EventToolResult, ConversationID: "conv-1", MessageID: "msg-tool", Tool: domain.ToolProductSearch, Status: "success", Content: "商品已查询", DataJSON: `{}`},
		{Type: domain.EventConfirmationRequired, ConversationID: "conv-1", MessageID: "msg-confirm", Tool: domain.ToolOrderCancel, Status: "pending", Content: "确认取消？", DataJSON: `{}`},
		{Type: domain.EventError, ConversationID: "conv-1", MessageID: "msg-error", Status: "failed", Content: "失败"},
		{Type: "unknown_event", ConversationID: "conv-1", MessageID: "msg-unknown", Content: "忽略"},
	})
	if err != nil {
		t.Fatalf("agentEventsToMessages returned error: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("messages len = %d, want 4", len(messages))
	}
	gotIDs := []string{messages[0].MsgId, messages[1].MsgId, messages[2].MsgId, messages[3].MsgId}
	wantIDs := []string{"msg-final", "msg-tool", "msg-confirm", "msg-error"}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("persisted ids = %+v, want %+v", gotIDs, wantIDs)
		}
	}
}

func TestRunSupervisorPersistsDurableEventsAndDoesNotForwardAssistantMessage(t *testing.T) {
	ctx := context.Background()
	messages := &confirmActionFakeMessagesModel{}
	runner := &chatFakeRunner{streamEvents: []domain.AgentEvent{
		{Type: domain.EventAssistantDelta, ConversationID: "conv-1", MessageID: "msg-delta", Content: "你"},
		{Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "msg-final", Content: "你好", Done: true},
		{Type: domain.EventToolProgress, ConversationID: "conv-1", MessageID: "msg-progress", Tool: domain.ToolProductSearch, Status: "running", Content: "正在查询商品..."},
		{Type: domain.EventToolResult, ConversationID: "conv-1", MessageID: "msg-tool", Tool: domain.ToolProductSearch, Status: "success", Content: "商品已查询", DataJSON: `{}`},
		{Type: domain.EventConfirmationRequired, ConversationID: "conv-1", MessageID: "msg-confirm", Tool: domain.ToolOrderCancel, Status: "pending", Content: "确认取消？", DataJSON: `{}`},
		{Type: domain.EventError, ConversationID: "conv-1", MessageID: "msg-error", Status: "failed", Content: "失败"},
	}}
	logic := NewChatLogic(ctx, &svc.ServiceContext{
		AgentRunner:   runner,
		MessagesModel: messages,
	})
	stream := &confirmActionFakeStream{ctx: ctx}

	persisted, err := logic.runSupervisor(&aiagent.ChatRequest{
		UserId:          42,
		ClientMessageId: "client-1",
	}, &conversation.PreparedConversation{
		ConversationID:  "conv-1",
		ClientMessageID: "client-1",
		UserMessageID:   "msg-user",
	}, nil, stream)
	if err != nil {
		t.Fatalf("runSupervisor returned error: %v", err)
	}
	if len(persisted) != 4 || len(messages.inserted) != 4 {
		t.Fatalf("persisted len=%d inserted len=%d, want 4", len(persisted), len(messages.inserted))
	}
	insertedIDs := []string{messages.inserted[0].MsgId, messages.inserted[1].MsgId, messages.inserted[2].MsgId, messages.inserted[3].MsgId}
	wantInsertedIDs := []string{"msg-final", "msg-tool", "msg-confirm", "msg-error"}
	for i := range wantInsertedIDs {
		if insertedIDs[i] != wantInsertedIDs[i] {
			t.Fatalf("inserted ids = %+v, want %+v", insertedIDs, wantInsertedIDs)
		}
	}
	sentTypes := make([]string, 0, len(stream.events))
	for _, event := range stream.events {
		sentTypes = append(sentTypes, event.Type)
		if event.Type == domain.EventAssistantMessage {
			t.Fatalf("assistant_message should not be forwarded, sent events=%+v", stream.events)
		}
	}
	wantSentTypes := []string{
		domain.EventAssistantDelta,
		domain.EventToolProgress,
		domain.EventToolResult,
		domain.EventConfirmationRequired,
		domain.EventError,
	}
	if len(sentTypes) != len(wantSentTypes) {
		t.Fatalf("sent types = %+v, want %+v", sentTypes, wantSentTypes)
	}
	for i := range wantSentTypes {
		if sentTypes[i] != wantSentTypes[i] {
			t.Fatalf("sent types = %+v, want %+v", sentTypes, wantSentTypes)
		}
	}
}

type chatFakeRunner struct {
	streamEvents []domain.AgentEvent
}

func (r *chatFakeRunner) Run(context.Context, eino.RunRequest) ([]domain.AgentEvent, error) {
	panic("not used")
}

func (r *chatFakeRunner) Resume(context.Context, eino.ResumeRequest) ([]domain.AgentEvent, error) {
	panic("not used")
}

func (r *chatFakeRunner) Stream(context.Context, eino.RunRequest) (<-chan domain.AgentEvent, error) {
	ch := make(chan domain.AgentEvent, len(r.streamEvents))
	for _, event := range r.streamEvents {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (r *chatFakeRunner) ResumeStream(context.Context, eino.ResumeRequest) (<-chan domain.AgentEvent, error) {
	panic("not used")
}
