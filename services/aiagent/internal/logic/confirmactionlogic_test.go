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
		t.Fatalf("stream events len = %d, want 2; events=%+v", len(stream.events), stream.events)
	}
	if stream.events[0].Type != domain.EventToolResult || stream.events[0].Tool != domain.ToolCartDelete || stream.events[0].Status != confirmation.StatusRejected {
		t.Fatalf("first event = %+v, want rejected tool_result", stream.events[0])
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(stream.events[0].DataJson), &data); err != nil {
		t.Fatalf("tool_result data_json is not valid JSON: %v", err)
	}
	if data["tool_name"] != domain.ToolCartDelete || data["status"] != confirmation.StatusRejected || data["summary"] != "操作已取消。" {
		t.Fatalf("tool_result data_json = %+v, want rejected cancellation payload", data)
	}
	if stream.events[1].Type != domain.EventAssistantMessage || stream.events[1].Content == "" {
		t.Fatalf("second event = %+v, want assistant_message cancellation summary", stream.events[1])
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
	if len(stream.events) != 2 {
		t.Fatalf("stream events len = %d, want 2; events=%+v", len(stream.events), stream.events)
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
	markExecutedCalls int
	markFailedCalls   int
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
	return nil, errors.New("ResumeStream should not be called")
}

type confirmActionFakeMessagesModel struct {
	inserted []*aimessages.AiMessages
}

func (m *confirmActionFakeMessagesModel) InsertBatch(_ context.Context, messages []*aimessages.AiMessages) error {
	m.inserted = append(m.inserted, messages...)
	return nil
}

func (m *confirmActionFakeMessagesModel) Insert(context.Context, *aimessages.AiMessages) (sql.Result, error) {
	panic("not used")
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

func (m *confirmActionFakeMessagesModel) FindRecentToolMessages(context.Context, uint64, string, int) ([]*aimessages.AiMessages, error) {
	panic("not used")
}

func (m *confirmActionFakeMessagesModel) FindToolMessageByID(context.Context, uint64, string, string) (*aimessages.AiMessages, error) {
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
