package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/utils/bizerr"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

func TestExecutorRetriesTransientFailureThenSucceeds(t *testing.T) {
	attempts := 0
	tool := core.Tool{
		Name:       "retry_query",
		Kind:       core.ToolKindBusiness,
		Visibility: core.ToolVisibilitySubAgents,
		Metadata: domain.Metadata{
			Name:           "retry_query",
			Risk:           domain.RiskLow,
			TimeoutSeconds: 1,
		},
		RetryPolicy: core.RetryPolicy{
			MaxRetries:        2,
			InitialDelay:      time.Millisecond,
			MaxDelay:          time.Millisecond,
			BackoffMultiplier: 1,
			MaxElapsed:        5 * time.Second,
		},
		Handler: func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
			attempts++
			if attempts < 3 {
				return core.HandlerResult{}, helper.ErrQueryRPCUnavailable
			}
			return core.HandlerResult{Data: map[string]any{"ok": true}, Summary: "ok"}, nil
		},
	}
	registry := NewRegistry([]core.Tool{tool})
	executor := NewExecutor(registry)

	event := executor.Execute(context.Background(), core.ExecuteRequest{
		UserID:         1,
		ConversationID: "conv-1",
		ToolName:       tool.Name,
	}, tool.Handler)

	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if event.Status != toolStatusSuccess {
		t.Fatalf("status = %q, want success; event=%+v", event.Status, event)
	}
	var envelope toolResultEnvelope
	if err := json.Unmarshal([]byte(event.DataJSON), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; data=%s", err, event.DataJSON)
	}
	if envelope.Status != toolStatusSuccess || envelope.AttemptCount != 3 || envelope.RetryCount != 2 {
		t.Fatalf("envelope = %+v, want success with attempts/retries", envelope)
	}
}

func TestExecutorDoesNotRetryPermanentFailure(t *testing.T) {
	attempts := 0
	tool := core.Tool{
		Name:       "permanent_query",
		Kind:       core.ToolKindBusiness,
		Visibility: core.ToolVisibilitySubAgents,
		Metadata: domain.Metadata{
			Name:           "permanent_query",
			Risk:           domain.RiskLow,
			TimeoutSeconds: 1,
		},
		RetryPolicy: core.RetryPolicy{
			MaxRetries:        2,
			InitialDelay:      time.Millisecond,
			MaxDelay:          time.Millisecond,
			BackoffMultiplier: 1,
			MaxElapsed:        5 * time.Second,
		},
		Handler: func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
			attempts++
			return core.HandlerResult{}, helper.InvalidArgument("keyword", "is required")
		},
	}
	registry := NewRegistry([]core.Tool{tool})
	executor := NewExecutor(registry)

	event := executor.Execute(context.Background(), core.ExecuteRequest{
		UserID:         1,
		ConversationID: "conv-1",
		ToolName:       tool.Name,
	}, tool.Handler)

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if event.Status != toolStatusFailed {
		t.Fatalf("status = %q, want failed", event.Status)
	}
	var envelope toolResultEnvelope
	if err := json.Unmarshal([]byte(event.DataJSON), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; data=%s", err, event.DataJSON)
	}
	if envelope.Error == nil || envelope.Error.Kind != string(ToolErrorKindPermanent) || envelope.RetryCount != 0 {
		t.Fatalf("envelope = %+v, want permanent failure without retry", envelope)
	}
}

func TestExecutorDoesNotRetryWriteTool(t *testing.T) {
	attempts := 0
	tool := core.Tool{
		Name:       "write_tool",
		Kind:       core.ToolKindBusiness,
		Visibility: core.ToolVisibilitySubAgents,
		Metadata: domain.Metadata{
			Name:           "write_tool",
			Risk:           domain.RiskLow,
			WriteOperation: true,
			TimeoutSeconds: 1,
		},
		RetryPolicy: core.RetryPolicy{
			MaxRetries:        2,
			InitialDelay:      time.Millisecond,
			MaxDelay:          time.Millisecond,
			BackoffMultiplier: 1,
			MaxElapsed:        5 * time.Second,
		},
		Handler: func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
			attempts++
			return core.HandlerResult{}, helper.ErrQueryRPCUnavailable
		},
	}
	registry := NewRegistry([]core.Tool{tool})
	executor := NewExecutor(registry)

	event := executor.Execute(context.Background(), core.ExecuteRequest{UserID: 1, ConversationID: "conv-1", ToolName: tool.Name}, tool.Handler)

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for write tool", attempts)
	}
	var envelope toolResultEnvelope
	if err := json.Unmarshal([]byte(event.DataJSON), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; data=%s", err, event.DataJSON)
	}
	if envelope.BusinessOutcome != BusinessOutcomeNotExecuted || envelope.RetryCount != 0 {
		t.Fatalf("envelope = %+v, want not_executed without retry", envelope)
	}
}

func TestExecutorDoesNotRetryBizerrAborted(t *testing.T) {
	attempts := 0
	tool := core.Tool{
		Name:       "bizerr_query",
		Kind:       core.ToolKindBusiness,
		Visibility: core.ToolVisibilitySubAgents,
		Metadata: domain.Metadata{
			Name:           "bizerr_query",
			Risk:           domain.RiskLow,
			TimeoutSeconds: 1,
		},
		RetryPolicy: core.RetryPolicy{
			MaxRetries:        2,
			InitialDelay:      time.Millisecond,
			MaxDelay:          time.Millisecond,
			BackoffMultiplier: 1,
			MaxElapsed:        5 * time.Second,
		},
		Handler: func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
			attempts++
			return core.HandlerResult{}, bizerr.Aborted(40001, "库存不足")
		},
	}
	registry := NewRegistry([]core.Tool{tool})
	executor := NewExecutor(registry)

	event := executor.Execute(context.Background(), core.ExecuteRequest{UserID: 1, ConversationID: "conv-1", ToolName: tool.Name}, tool.Handler)

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for bizerr.Aborted", attempts)
	}
	var envelope toolResultEnvelope
	if err := json.Unmarshal([]byte(event.DataJSON), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; data=%s", err, event.DataJSON)
	}
	if envelope.Error == nil || envelope.Error.Code != "business_40001" || envelope.Error.Message != "库存不足" {
		t.Fatalf("envelope error = %+v, want parsed business error", envelope.Error)
	}
}

func TestExecutorDoesNotExposeRawErrorToModel(t *testing.T) {
	tool := core.Tool{
		Name:       "secret_query",
		Kind:       core.ToolKindBusiness,
		Visibility: core.ToolVisibilitySubAgents,
		Metadata: domain.Metadata{
			Name:           "secret_query",
			Risk:           domain.RiskLow,
			TimeoutSeconds: 1,
		},
		Handler: func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
			return core.HandlerResult{}, errors.New("dial mysql root:secret@tcp(10.0.0.1)")
		},
	}
	registry := NewRegistry([]core.Tool{tool})
	executor := NewExecutor(registry)

	event := executor.Execute(context.Background(), core.ExecuteRequest{UserID: 1, ConversationID: "conv-1", ToolName: tool.Name}, tool.Handler)

	if strings.Contains(event.DataJSON, "root:secret") || strings.Contains(event.Content, "10.0.0.1") {
		t.Fatalf("tool event leaked raw error: %+v", event)
	}
}

func TestInvokableRunReturnsExpectedToolFailureAsToolMessage(t *testing.T) {
	tool := core.Tool{
		Name:       "adapter_query",
		Kind:       core.ToolKindBusiness,
		Visibility: core.ToolVisibilitySubAgents,
		Metadata: domain.Metadata{
			Name:           "adapter_query",
			Risk:           domain.RiskLow,
			TimeoutSeconds: 1,
		},
		Handler: func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
			return core.HandlerResult{}, helper.InvalidArgument("keyword", "is required")
		},
	}
	registry := NewRegistry([]core.Tool{tool})
	executor := NewExecutor(registry)
	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{
		UserID:         1,
		ConversationID: "conv-1",
		ToolCallID:     "call-1",
	})

	output, err := (&invokableToolAdapter{tool: tool, executor: executor}).InvokableRun(ctx, `{"keyword":""}`)
	if err != nil {
		t.Fatalf("InvokableRun error = %v, want nil for expected tool failure", err)
	}
	var envelope toolResultEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; output=%s", err, output)
	}
	if envelope.Status != toolStatusFailed {
		t.Fatalf("envelope status = %q, want failed", envelope.Status)
	}
}

func TestInvokableRunKeepsSystemInvariantErrorsAsGoErrors(t *testing.T) {
	tool := core.Tool{Name: "adapter_query"}
	_, err := (&invokableToolAdapter{tool: tool, executor: NewExecutor(NewRegistry([]core.Tool{tool}))}).InvokableRun(context.Background(), `{}`)
	if !errors.Is(err, helper.ErrToolExecutionContext) {
		t.Fatalf("err = %v, want trusted context error", err)
	}
}
