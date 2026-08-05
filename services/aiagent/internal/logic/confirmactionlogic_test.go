package logic

import (
	"context"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
)

func TestConfirmActionApprovedResumesCheckpointAndMarksExecuted(t *testing.T) {
	manager := &fakeConfirmManager{decision: confirmationWithResumeTarget()}
	runner := &fakeConfirmRunner{events: []domain.AgentEvent{{
		Type:             domain.EventToolResult,
		ConversationID:   "conv-1",
		Tool:             domain.ToolOrderCancel,
		Status:           "success",
		Content:          "订单已取消。",
		Done:             true,
		BusinessExecuted: true,
	}}}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager,
		AgentRunner:         runner,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId:         42,
		ConversationId: "conv-1",
		ConfirmationId: "confirm-1",
		Approved:       true,
	})
	if err != nil {
		t.Fatalf("ConfirmAction error = %v", err)
	}
	if resp.StatusCode != 0 || manager.markExecutedCalls != 1 || manager.markFailedCalls != 0 {
		t.Fatalf("resp=%#v executed=%d failed=%d", resp, manager.markExecutedCalls, manager.markFailedCalls)
	}
	if !runner.resumeReq.Approved || runner.resumeReq.CheckpointID != "checkpoint-1" || runner.resumeReq.InterruptID != "interrupt-1" {
		t.Fatalf("resume request = %#v", runner.resumeReq)
	}
}

func TestConfirmActionRejectedResumesWithoutCompletionMark(t *testing.T) {
	manager := &fakeConfirmManager{decision: confirmationWithResumeTarget()}
	runner := &fakeConfirmRunner{events: []domain.AgentEvent{{
		Type:           domain.EventToolResult,
		ConversationID: "conv-1",
		Tool:           domain.ToolOrderCancel,
		Status:         "rejected",
		Content:        "操作已取消。",
		Done:           true,
	}}}
	logic := NewConfirmActionLogic(context.Background(), &svc.ServiceContext{
		ConfirmationManager: manager,
		AgentRunner:         runner,
	})

	resp, err := logic.ConfirmAction(&aiagent.ConfirmActionRequest{
		UserId:         42,
		ConversationId: "conv-1",
		ConfirmationId: "confirm-1",
		Approved:       false,
	})
	if err != nil {
		t.Fatalf("ConfirmAction error = %v", err)
	}
	if resp.StatusCode != 0 || manager.markExecutedCalls != 0 || manager.markFailedCalls != 0 {
		t.Fatalf("resp=%#v executed=%d failed=%d", resp, manager.markExecutedCalls, manager.markFailedCalls)
	}
	if runner.resumeReq.Approved {
		t.Fatalf("resume request approved = true, want false")
	}
}

func confirmationWithResumeTarget() *domain.Confirmation {
	return &domain.Confirmation{
		ID:             "confirm-1",
		UserID:         42,
		ConversationID: "conv-1",
		ToolName:       domain.ToolOrderCancel,
		Status:         confirmation.StatusApproved,
		RunID:          "run-1",
		CheckpointID:   "checkpoint-1",
		InterruptID:    "interrupt-1",
	}
}

type fakeConfirmManager struct {
	decision          *domain.Confirmation
	markExecutedCalls int
	markFailedCalls   int
}

func (m *fakeConfirmManager) Decide(context.Context, confirmation.DecisionRequest) (*domain.Confirmation, error) {
	return m.decision, nil
}

func (m *fakeConfirmManager) MarkExecuted(context.Context, confirmation.CompletionRequest) (*domain.Confirmation, error) {
	m.markExecutedCalls++
	return m.decision, nil
}

func (m *fakeConfirmManager) MarkFailed(context.Context, confirmation.CompletionRequest) (*domain.Confirmation, error) {
	m.markFailedCalls++
	return m.decision, nil
}

func (m *fakeConfirmManager) BindResumeTarget(context.Context, confirmation.ResumeTargetRequest) (*domain.Confirmation, error) {
	return m.decision, nil
}

type fakeConfirmRunner struct {
	resumeReq eino.ResumeRequest
	events    []domain.AgentEvent
}

func (r *fakeConfirmRunner) Run(context.Context, eino.RunRequest) ([]domain.AgentEvent, error) {
	return nil, nil
}

func (r *fakeConfirmRunner) Resume(_ context.Context, req eino.ResumeRequest) ([]domain.AgentEvent, error) {
	r.resumeReq = req
	return r.events, nil
}

func (r *fakeConfirmRunner) Stream(context.Context, eino.RunRequest) (<-chan domain.AgentEvent, error) {
	ch := make(chan domain.AgentEvent)
	close(ch)
	return ch, nil
}
