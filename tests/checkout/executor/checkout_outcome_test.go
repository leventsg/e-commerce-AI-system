//go:build checkout_internal_tests

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	business "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	rpc "github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type outcomeCheckoutRPC struct {
	status      int32
	err         error
	calls       int
	nilResponse bool
}

func (f *outcomeCheckoutRPC) IssueCheckoutToken(context.Context, *rpc.IssueCheckoutTokenReq, ...grpc.CallOption) (*rpc.IssueCheckoutTokenResp, error) {
	return &rpc.IssueCheckoutTokenResp{CheckoutToken: "server-token"}, nil
}
func (f *outcomeCheckoutRPC) PrepareCheckout(context.Context, *rpc.CheckoutReq, ...grpc.CallOption) (*rpc.CheckoutResp, error) {
	f.calls++
	if f.nilResponse {
		return nil, nil
	}
	return &rpc.CheckoutResp{StatusCode: f.status, StatusMsg: "pending reconciliation"}, f.err
}

type outcomeRecorder struct{ records []ToolCallRecord }

func (r *outcomeRecorder) RecordToolCall(_ context.Context, record ToolCallRecord) error {
	r.records = append(r.records, record)
	return nil
}

func TestCheckoutExecutorKeepsUncertainOutcome(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int32
		err         error
		nilResponse bool
	}{
		{"processing", code.CheckoutProcessing, nil, false},
		{"unknown", code.CheckoutOutcomeUnknown, nil, false},
		{"transport", 0, status.Error(codes.Unavailable, "lost response"), false},
		{"nil_response", 0, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &outcomeCheckoutRPC{status: test.status, err: test.err, nilResponse: test.nilResponse}
			handler := business.CheckoutWriteHandlers(fake)[domain.ToolCheckoutPrepare]
			tool := core.Tool{Name: domain.ToolCheckoutPrepare, Kind: core.ToolKindBusiness, Metadata: domain.Metadata{Name: domain.ToolCheckoutPrepare, WriteOperation: true, TimeoutSeconds: 1}, Handler: handler}
			recorder := &outcomeRecorder{}
			executor := NewExecutor(NewRegistry([]core.Tool{tool}), WithToolCallRecorder(recorder))
			ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{UserID: 7, ConversationID: "conv", RunID: "run", ToolCallID: "call"})
			event := executor.Execute(ctx, core.ExecuteRequest{UserID: 7, ConversationID: "conv", ToolCallID: "call", ToolName: tool.Name, Arguments: map[string]any{"order_items": []any{map[string]any{"product_id": 42, "quantity": 1}}}}, handler)
			var envelope toolResultEnvelope
			if err := json.Unmarshal([]byte(event.DataJSON), &envelope); err != nil {
				t.Fatal(err)
			}
			if event.Status != "failed" || envelope.BusinessOutcome != "unknown" || envelope.RetryCount != 0 || fake.calls != 1 {
				t.Fatalf("uncertain checkout must not claim not_executed or retry: event=%+v envelope=%+v calls=%d", event, envelope, fake.calls)
			}
			if len(recorder.records) != 1 {
				t.Fatal("write outcome audit missing")
			}
			payload, _ := json.Marshal(recorder.records[0].ResultData)
			if raw, ok := recorder.records[0].ResultData.(string); ok {
				payload = []byte(raw)
			}
			var audit toolResultEnvelope
			if err := json.Unmarshal(payload, &audit); err != nil {
				t.Fatal(err)
			}
			if audit.BusinessOutcome != "unknown" {
				t.Fatalf("audit lost uncertain outcome: %s", payload)
			}
		})
	}
}

func TestCheckoutCancellationCannotClaimNotExecuted(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	handler := func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
		close(started)
		<-release
		return core.HandlerResult{}, context.Canceled
	}
	tool := core.Tool{Name: domain.ToolCheckoutPrepare, Kind: core.ToolKindBusiness, Metadata: domain.Metadata{Name: domain.ToolCheckoutPrepare, WriteOperation: true, TimeoutSeconds: 1}, Handler: handler}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	event := NewExecutor(NewRegistry([]core.Tool{tool})).Execute(ctx, core.ExecuteRequest{UserID: 7, ToolName: tool.Name}, handler)
	var envelope toolResultEnvelope
	if err := json.Unmarshal([]byte(event.DataJSON), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.BusinessOutcome != "unknown" {
		t.Fatalf("cancelled checkout may still run: %+v", envelope)
	}
}
