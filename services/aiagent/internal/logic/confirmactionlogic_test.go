package logic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

func TestConfirmActionRejectsWithoutExecutingTool(t *testing.T) {
	manager := &fakeConfirmActionManager{decided: &domain.Confirmation{
		ID: "confirm-1", UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCancel,
		Status: confirmation.StatusRejected,
	}}
	executor := &fakeConfirmedToolExecutor{}
	messages := &fakeChatMessagesModel{}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager, HighRiskTools: executor, MessagesModel: messages,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: false,
	})

	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if executor.calls != 0 || manager.failedCalls != 0 || manager.executedCalls != 0 {
		t.Fatalf("unexpected execution: executor=%d failed=%d executed=%d", executor.calls, manager.failedCalls, manager.executedCalls)
	}
	if len(resp.Events) != 1 || resp.Events[0].Type != domain.EventAssistantMessage || !resp.Events[0].Done {
		t.Fatalf("response = %#v", resp)
	}
	if messages.batchCalls != 1 || messages.insertCalls != 0 || len(messages.inserted) != 1 || messages.inserted[0].Role != "assistant" || messages.inserted[0].ConversationId != "conv-1" {
		t.Fatalf("batchCalls=%d insertCalls=%d messages=%+v", messages.batchCalls, messages.insertCalls, messages.inserted)
	}
}

func TestConfirmActionApprovedExecutesPersistedArgumentsAndMarksExecuted(t *testing.T) {
	manager := &fakeConfirmActionManager{decided: approvedConfirmation()}
	executor := &fakeConfirmedToolExecutor{event: domain.AgentEvent{
		Type: domain.EventToolResult, Tool: domain.ToolOrderCancel, Status: "success",
		DataJSON: `{"order_id":"order-1"}`, Content: "订单已取消。", Done: true,
	}}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager, HighRiskTools: executor,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: true,
	})

	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if executor.calls != 1 || executor.req.UserID != 42 || executor.req.ToolName != domain.ToolOrderCancel {
		t.Fatalf("execute request = %#v calls=%d", executor.req, executor.calls)
	}
	if executor.req.Arguments["order_id"] != "order-1" {
		t.Fatalf("execute arguments = %#v", executor.req.Arguments)
	}
	if manager.executedCalls != 1 || manager.failedCalls != 0 {
		t.Fatalf("completion calls: executed=%d failed=%d", manager.executedCalls, manager.failedCalls)
	}
	if len(resp.Events) != 1 || resp.Events[0].Type != domain.EventToolResult || resp.Events[0].Status != "success" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestConfirmActionPreservesExecutedResultWhenBatchPersistenceFails(t *testing.T) {
	manager := &fakeConfirmActionManager{decided: approvedConfirmation()}
	executor := &fakeConfirmedToolExecutor{event: domain.AgentEvent{
		Type: domain.EventToolResult, Tool: domain.ToolOrderCancel, Status: "success",
		DataJSON: `{"order_id":"order-1"}`, Content: "订单已取消。", BusinessExecuted: true, Done: true,
	}}
	messages := &fakeChatMessagesModel{err: errors.New("batch insert failed")}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager, HighRiskTools: executor, MessagesModel: messages,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: true,
	})
	if err != nil || len(resp.Events) != 2 || resp.Events[0].Type != domain.EventToolResult || resp.Events[1].Type != domain.EventError {
		t.Fatalf("ConfirmAction() resp=%+v err=%v", resp, err)
	}
	if messages.batchCalls != 1 || messages.insertCalls != 0 || len(messages.inserted) != 1 {
		t.Fatalf("batchCalls=%d insertCalls=%d messages=%+v", messages.batchCalls, messages.insertCalls, messages.inserted)
	}
	if !strings.Contains(resp.Events[1].DataJson, `"business_executed":true`) {
		t.Fatalf("persistence error=%+v", resp.Events[1])
	}
}

func TestConfirmActionFailedToolMarksConfirmationFailed(t *testing.T) {
	manager := &fakeConfirmActionManager{decided: approvedConfirmation()}
	executor := &fakeConfirmedToolExecutor{event: domain.AgentEvent{
		Type: domain.EventToolResult, Tool: domain.ToolOrderCancel, Status: "failed", Content: "取消失败。", Done: true,
	}}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager, HighRiskTools: executor,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: true,
	})

	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if manager.failedCalls != 1 || manager.executedCalls != 0 {
		t.Fatalf("completion calls: failed=%d executed=%d", manager.failedCalls, manager.executedCalls)
	}
	if len(resp.Events) != 1 || resp.Events[0].Status != "failed" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestConfirmActionAuditFailureAfterBusinessExecutionMarksExecuted(t *testing.T) {
	manager := &fakeConfirmActionManager{decided: approvedConfirmation()}
	executor := &fakeConfirmedToolExecutor{event: domain.AgentEvent{
		Type: domain.EventToolResult, Tool: domain.ToolOrderCancel, Status: "failed",
		Content: "操作已完成，但审计记录失败。", BusinessExecuted: true, Done: true,
	}}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager, HighRiskTools: executor,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: true,
	})
	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if manager.executedCalls != 1 || manager.failedCalls != 0 {
		t.Fatalf("completion calls: executed=%d failed=%d", manager.executedCalls, manager.failedCalls)
	}
	if len(resp.Events) != 1 || resp.Events[0].Status != "failed" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestConfirmActionCompletionPersistenceFailureAppendsErrorEvent(t *testing.T) {
	manager := &fakeConfirmActionManager{decided: approvedConfirmation(), executedErr: errors.New("mysql unavailable")}
	executor := &fakeConfirmedToolExecutor{event: domain.AgentEvent{
		Type: domain.EventToolResult, Tool: domain.ToolOrderCancel, Status: "success", BusinessExecuted: true, Done: true,
	}}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager, HighRiskTools: executor,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: true,
	})
	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if len(resp.Events) != 2 || resp.Events[0].Status != "success" || resp.Events[1].Type != domain.EventError {
		t.Fatalf("response = %#v", resp)
	}
}

func TestConfirmActionDecisionErrorsReturnErrorEventAndNeverExecute(t *testing.T) {
	for _, decisionErr := range []error{
		confirmation.ErrConfirmationExpired,
		confirmation.ErrConfirmationAlreadyProcessed,
		confirmation.ErrConfirmationForbidden,
		confirmation.ErrConfirmationBusy,
	} {
		t.Run(decisionErr.Error(), func(t *testing.T) {
			manager := &fakeConfirmActionManager{decideErr: decisionErr}
			executor := &fakeConfirmedToolExecutor{}
			logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
				ConfirmationManager: manager, HighRiskTools: executor,
			})
			resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
				UserId: 42, ConversationId: "conv-1", ConfirmationId: "confirm-1", Approved: true,
			})
			if err != nil {
				t.Fatalf("ConfirmAction: %v", err)
			}
			if executor.calls != 0 {
				t.Fatalf("executor calls = %d", executor.calls)
			}
			if resp.StatusCode == 0 || len(resp.Events) != 1 || resp.Events[0].Type != domain.EventError {
				t.Fatalf("response = %#v", resp)
			}
		})
	}
}

func TestConfirmActionValidatesTrustedRequestFields(t *testing.T) {
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: &fakeConfirmActionManager{}, HighRiskTools: &fakeConfirmedToolExecutor{},
	})
	for _, req := range []*aiagent.ConfirmActionRequest{
		nil,
		{ConversationId: "conv-1", ConfirmationId: "confirm-1"},
		{UserId: 42, ConfirmationId: "confirm-1"},
		{UserId: 42, ConversationId: "conv-1"},
	} {
		resp, err := logic.ConfirmAction(req)
		if err != nil {
			t.Fatalf("ConfirmAction: %v", err)
		}
		if resp.StatusCode == 0 || len(resp.Events) != 1 || resp.Events[0].Type != domain.EventError {
			t.Fatalf("response = %#v", resp)
		}
	}
}

func approvedConfirmation() *domain.Confirmation {
	return &domain.Confirmation{
		ID: "confirm-1", UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCancel,
		Arguments: map[string]any{"order_id": "order-1"}, Status: confirmation.StatusApproved,
	}
}

type fakeConfirmActionManager struct {
	decided       *domain.Confirmation
	decideErr     error
	executedErr   error
	failedErr     error
	decisionReq   confirmation.DecisionRequest
	executedCalls int
	failedCalls   int
}

func (f *fakeConfirmActionManager) Decide(_ context.Context, req confirmation.DecisionRequest) (*domain.Confirmation, error) {
	f.decisionReq = req
	return f.decided, f.decideErr
}

func (f *fakeConfirmActionManager) MarkExecuted(_ context.Context, _ confirmation.CompletionRequest) (*domain.Confirmation, error) {
	f.executedCalls++
	return f.decided, f.executedErr
}

func (f *fakeConfirmActionManager) MarkFailed(_ context.Context, _ confirmation.CompletionRequest) (*domain.Confirmation, error) {
	f.failedCalls++
	return f.decided, f.failedErr
}

type fakeConfirmedToolExecutor struct {
	req   tools.ExecuteRequest
	event domain.AgentEvent
	calls int
}

func (f *fakeConfirmedToolExecutor) ExecuteConfirmed(_ context.Context, req tools.ExecuteRequest) domain.AgentEvent {
	f.calls++
	f.req = req
	return f.event
}
