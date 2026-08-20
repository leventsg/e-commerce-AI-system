package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
)

func TestConfirmActionRejectsWithoutResumingAgent(t *testing.T) {
	ctx := context.Background()
	manager := &confirmActionFakeConfirmationManager{
		decided: &domain.Confirmation{
			ID:             "confirm-1",
			ConversationID: "conv-1",
			UserID:         42,
			ToolName:       domain.ToolCartDelete,
			Status:         confirmation.StatusRejected,
			ExpiresAt:      time.Now().Add(time.Minute),
		},
	}
	runner := &confirmActionFakeRunner{}
	messages := &confirmActionFakeMessagesModel{}
	logic := NewConfirmActionLogic(ctx, &svc.ServiceContext{
		ConfirmationManager: manager,
		AgentRunner:         runner,
		MessagesModel:       messages,
	})
	stream := &confirmActionFakeStream{ctx: ctx}

	err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId:         42,
		ConversationId: "conv-1",
		ConfirmationId: "confirm-1",
		Approved:       false,
	}, stream)
	if err != nil {
		t.Fatalf("ConfirmAction returned error: %v", err)
	}
	if runner.resumeStreamCalls != 0 {
		t.Fatalf("ResumeStream calls = %d, want 0 for rejected confirmation", runner.resumeStreamCalls)
	}
	if manager.markExecutedCalls != 0 || manager.markFailedCalls != 0 {
		t.Fatalf("completion calls executed=%d failed=%d, want 0", manager.markExecutedCalls, manager.markFailedCalls)
	}
	if len(stream.events) != 2 {
		t.Fatalf("stream events len = %d, want tool_result and assistant_message; events=%+v", len(stream.events), stream.events)
	}
	if stream.events[0].Type != domain.EventToolResult || stream.events[0].Tool != domain.ToolCartDelete || stream.events[0].Status != confirmation.StatusRejected {
		t.Fatalf("first event = %+v, want rejected tool_result", stream.events[0])
	}
	if stream.events[1].Type != domain.EventAssistantMessage || stream.events[1].Content == "" {
		t.Fatalf("second event = %+v, want cancellation assistant_message", stream.events[1])
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(stream.events[0].DataJson), &data); err != nil {
		t.Fatalf("tool_result data_json is not valid JSON: %v", err)
	}
	if data["tool_name"] != domain.ToolCartDelete || data["status"] != confirmation.StatusRejected || data["summary"] != "操作已取消。" {
		t.Fatalf("tool_result data_json = %+v, want rejected cancellation payload", data)
	}
	if len(messages.inserted) != 2 {
		t.Fatalf("inserted messages len = %d, want 2", len(messages.inserted))
	}
	if messages.inserted[0].Role != "tool" || messages.inserted[1].Role != "assistant" {
		t.Fatalf("inserted roles = %q, %q; want tool, assistant", messages.inserted[0].Role, messages.inserted[1].Role)
	}
}

func TestConfirmActionRejectDoesNotRequireAgentRunner(t *testing.T) {
	ctx := context.Background()
	manager := &confirmActionFakeConfirmationManager{
		decided: &domain.Confirmation{
			ID:             "confirm-1",
			ConversationID: "conv-1",
			UserID:         42,
			ToolName:       domain.ToolCartDelete,
			Status:         confirmation.StatusRejected,
		},
	}
	messages := &confirmActionFakeMessagesModel{}
	logic := NewConfirmActionLogic(ctx, &svc.ServiceContext{
		ConfirmationManager: manager,
		MessagesModel:       messages,
	})
	stream := &confirmActionFakeStream{ctx: ctx}

	err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId:         42,
		ConversationId: "conv-1",
		ConfirmationId: "confirm-1",
		Approved:       false,
	}, stream)
	if err != nil {
		t.Fatalf("ConfirmAction returned error: %v", err)
	}
	if len(stream.events) != 1 {
		t.Fatalf("stream events len = %d, want only tool_result; events=%+v", len(stream.events), stream.events)
	}
}

func TestConfirmActionApprovePersistsDurableEventsAndForwardsAssistantMessage(t *testing.T) {
	ctx := context.Background()
	manager := &confirmActionFakeConfirmationManager{
		decided: &domain.Confirmation{
			ID:             "confirm-1",
			ConversationID: "conv-1",
			UserID:         42,
			ToolName:       domain.ToolOrderCancel,
			Status:         confirmation.StatusApproved,
			RunID:          "run-1",
			CheckpointID:   "checkpoint-1",
			InterruptID:    "interrupt-1",
		},
	}
	runner := &confirmActionFakeRunner{resumeEvents: []domain.AgentEvent{
		{Type: domain.EventAssistantDelta, ConversationID: "conv-1", MessageID: "msg-delta", Content: "已"},
		{Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "msg-final", Content: "已取消订单。", Done: true},
		{Type: domain.EventToolProgress, ConversationID: "conv-1", MessageID: "msg-progress", Tool: domain.ToolOrderCancel, Status: "running", Content: "正在处理订单..."},
		{Type: domain.EventToolResult, ConversationID: "conv-1", MessageID: "msg-tool", Tool: domain.ToolOrderCancel, Status: "success", Content: "订单已取消。", DataJSON: `{}`, BusinessExecuted: true},
		{Type: domain.EventError, ConversationID: "conv-1", MessageID: "msg-error", Status: "failed", Content: "后续总结失败"},
	}}
	messages := &confirmActionFakeMessagesModel{}
	logic := NewConfirmActionLogic(ctx, &svc.ServiceContext{
		ConfirmationManager: manager,
		AgentRunner:         runner,
		MessagesModel:       messages,
	})
	stream := &confirmActionFakeStream{ctx: ctx}

	err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId:         42,
		ConversationId: "conv-1",
		ConfirmationId: "confirm-1",
		Approved:       true,
	}, stream)
	if err != nil {
		t.Fatalf("ConfirmAction returned error: %v", err)
	}
	if runner.resumeStreamCalls != 1 {
		t.Fatalf("ResumeStream calls = %d, want 1", runner.resumeStreamCalls)
	}
	if manager.markExecutedCalls != 1 || manager.markFailedCalls != 0 {
		t.Fatalf("completion calls executed=%d failed=%d, want executed=1 failed=0", manager.markExecutedCalls, manager.markFailedCalls)
	}
	if len(messages.inserted) != 3 {
		t.Fatalf("inserted messages len = %d, want 3", len(messages.inserted))
	}
	insertedIDs := []string{messages.inserted[0].MsgId, messages.inserted[1].MsgId, messages.inserted[2].MsgId}
	wantInsertedIDs := []string{"msg-final", "msg-tool", "msg-error"}
	for i := range wantInsertedIDs {
		if insertedIDs[i] != wantInsertedIDs[i] {
			t.Fatalf("inserted ids = %+v, want %+v", insertedIDs, wantInsertedIDs)
		}
	}
	sentTypes := make([]string, 0, len(stream.events))
	for _, event := range stream.events {
		sentTypes = append(sentTypes, event.Type)
	}
	wantSentTypes := []string{
		domain.EventAssistantDelta,
		domain.EventAssistantMessage,
		domain.EventToolProgress,
		domain.EventToolResult,
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

func TestConfirmActionApproveForwardsAssistantMessageWithoutDelta(t *testing.T) {
	ctx := context.Background()
	manager := &confirmActionFakeConfirmationManager{
		decided: &domain.Confirmation{
			ID:             "confirm-1",
			ConversationID: "conv-1",
			UserID:         42,
			ToolName:       domain.ToolOrderCancel,
			Status:         confirmation.StatusApproved,
			RunID:          "run-1",
			CheckpointID:   "checkpoint-1",
			InterruptID:    "interrupt-1",
		},
	}
	runner := &confirmActionFakeRunner{resumeEvents: []domain.AgentEvent{
		{Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "msg-final", Content: "已取消订单。", Done: true},
		{Type: domain.EventToolResult, ConversationID: "conv-1", MessageID: "msg-tool", Tool: domain.ToolOrderCancel, Status: "success", Content: "订单已取消。", DataJSON: `{}`, BusinessExecuted: true},
	}}
	messages := &confirmActionFakeMessagesModel{}
	logic := NewConfirmActionLogic(ctx, &svc.ServiceContext{
		ConfirmationManager: manager,
		AgentRunner:         runner,
		MessagesModel:       messages,
	})
	stream := &confirmActionFakeStream{ctx: ctx}

	err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId:         42,
		ConversationId: "conv-1",
		ConfirmationId: "confirm-1",
		Approved:       true,
	}, stream)
	if err != nil {
		t.Fatalf("ConfirmAction returned error: %v", err)
	}
	if len(messages.inserted) != 2 {
		t.Fatalf("inserted messages len = %d, want 2", len(messages.inserted))
	}
	if messages.inserted[0].MsgId != "msg-final" || messages.inserted[1].MsgId != "msg-tool" {
		t.Fatalf("inserted ids = %q, %q; want msg-final, msg-tool", messages.inserted[0].MsgId, messages.inserted[1].MsgId)
	}
	sentTypes := make([]string, 0, len(stream.events))
	for _, event := range stream.events {
		sentTypes = append(sentTypes, event.Type)
	}
	wantSentTypes := []string{domain.EventAssistantMessage, domain.EventToolResult}
	if len(sentTypes) != len(wantSentTypes) {
		t.Fatalf("sent types = %+v, want %+v", sentTypes, wantSentTypes)
	}
	for i := range wantSentTypes {
		if sentTypes[i] != wantSentTypes[i] {
			t.Fatalf("sent types = %+v, want %+v", sentTypes, wantSentTypes)
		}
	}
}

type confirmActionFakeStream struct {
	ctx    context.Context
	events []*aiagent.AgentEvent
}

func (s *confirmActionFakeStream) Send(event *aiagent.AgentEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *confirmActionFakeStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

type confirmActionFakeConfirmationManager struct {
	decided           *domain.Confirmation
	decideErr         error
	createCalls       int
	markExecutedCalls int
	markFailedCalls   int
}

func (m *confirmActionFakeConfirmationManager) Create(_ context.Context, _ confirmation.CreateRequest) (*domain.Confirmation, error) {
	m.createCalls++
	return m.decided, nil
}

func (m *confirmActionFakeConfirmationManager) Decide(_ context.Context, _ confirmation.DecisionRequest) (*domain.Confirmation, error) {
	return m.decided, m.decideErr
}

func (m *confirmActionFakeConfirmationManager) MarkExecuted(_ context.Context, _ confirmation.CompletionRequest) (*domain.Confirmation, error) {
	m.markExecutedCalls++
	return m.decided, nil
}

func (m *confirmActionFakeConfirmationManager) MarkFailed(_ context.Context, _ confirmation.CompletionRequest) (*domain.Confirmation, error) {
	m.markFailedCalls++
	return m.decided, nil
}

func (m *confirmActionFakeConfirmationManager) BindResumeTarget(_ context.Context, _ confirmation.ResumeTargetRequest) (*domain.Confirmation, error) {
	return m.decided, nil
}

type confirmActionFakeRunner struct {
	resumeStreamCalls int
	resumeEvents      []domain.AgentEvent
}

func (r *confirmActionFakeRunner) Run(context.Context, eino.RunRequest) ([]domain.AgentEvent, error) {
	return nil, errors.New("Run should not be called")
}

func (r *confirmActionFakeRunner) Resume(context.Context, eino.ResumeRequest) ([]domain.AgentEvent, error) {
	return nil, errors.New("Resume should not be called")
}

func (r *confirmActionFakeRunner) Stream(context.Context, eino.RunRequest) (<-chan domain.AgentEvent, error) {
	return nil, errors.New("Stream should not be called")
}

func (r *confirmActionFakeRunner) ResumeStream(context.Context, eino.ResumeRequest) (<-chan domain.AgentEvent, error) {
	r.resumeStreamCalls++
	if r.resumeEvents == nil {
		return nil, errors.New("ResumeStream should not be called")
	}
	ch := make(chan domain.AgentEvent, len(r.resumeEvents))
	for _, event := range r.resumeEvents {
		ch <- event
	}
	close(ch)
	return ch, nil
}

type confirmActionFakeMessagesModel struct {
	inserted []*aimessages.AiMessages
}

type fakeSQLResult int64

func (r fakeSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r fakeSQLResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

func (m *confirmActionFakeMessagesModel) InsertBatch(_ context.Context, messages []*aimessages.AiMessages) error {
	m.inserted = append(m.inserted, messages...)
	return nil
}

func (m *confirmActionFakeMessagesModel) Insert(_ context.Context, message *aimessages.AiMessages) (sql.Result, error) {
	m.inserted = append(m.inserted, message)
	return fakeSQLResult(1), nil
}

func (m *confirmActionFakeMessagesModel) FindOne(context.Context, uint64) (*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindOneByMsgId(context.Context, string) (*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindOneByUserIdDedupeClientMessageId(context.Context, uint64, sql.NullString) (*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) Update(context.Context, *aimessages.AiMessages) error {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) Delete(context.Context, uint64) error {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindRecentByConversationID(context.Context, string, int) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindRecentContextMessages(context.Context, uint64, string, int) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) CountUnsummarizedContextMessages(context.Context, uint64, string, string, string) (int64, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindUnsummarizedContextMessages(context.Context, uint64, string, string, string, int) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindRecentUnsummarizedContextMessages(context.Context, uint64, string, string, string, int) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindMessagesByIDs(context.Context, uint64, string, []string) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindUserMessageByClientMessageID(context.Context, uint64, string) (*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindAssistantMessagesByClientMessageID(context.Context, uint64, string, string) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindByUserAndConversation(context.Context, uint64, string, int, int) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) CountByUserAndConversation(context.Context, uint64, string) (int64, error) {
	panic("not used")
}
