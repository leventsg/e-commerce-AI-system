//go:build checkout_internal_tests

package checkout_tools

import (
	"context"
	"errors"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	rpc "github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"google.golang.org/grpc"
)

type intentRPC struct {
	intents    []*rpc.IssueCheckoutTokenReq
	prepares   []*rpc.CheckoutReq
	issueError error
}

func (f *intentRPC) IssueCheckoutToken(_ context.Context, in *rpc.IssueCheckoutTokenReq, _ ...grpc.CallOption) (*rpc.IssueCheckoutTokenResp, error) {
	f.intents = append(f.intents, in)
	if f.issueError != nil {
		return nil, f.issueError
	}
	return &rpc.IssueCheckoutTokenResp{CheckoutToken: "server-" + in.IntentId, ExpiresAt: 2000000000}, nil
}
func (f *intentRPC) PrepareCheckout(_ context.Context, in *rpc.CheckoutReq, _ ...grpc.CallOption) (*rpc.CheckoutResp, error) {
	f.prepares = append(f.prepares, in)
	return &rpc.CheckoutResp{PreOrderId: "pre-1", ExpireTime: 2000000000, PayMethod: []int64{1, 2}}, nil
}
func intentContext(call string) context.Context {
	return helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{UserID: 7, ConversationID: "conversation", RunID: "run-1", ToolCallID: call})
}
func intentRequest() core.HandlerRequest {
	return core.HandlerRequest{UserID: 7, ConversationID: "conversation", Arguments: map[string]any{"order_items": []any{map[string]any{"product_id": float64(42), "quantity": float64(1)}}, "user_id": 999, "checkout_token": "attacker-token"}}
}
func TestAITokenFollowsExecutionNotProduct(t *testing.T) {
	fake := &intentRPC{}
	handler := checkoutPrepareHandler(fake)
	for _, call := range []string{"call-1", "call-1", "call-2"} {
		if _, err := handler(intentContext(call), intentRequest()); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.intents) != 3 {
		t.Fatalf("must obtain server token before each prepare: %d", len(fake.intents))
	}
	if fake.intents[0].IntentId != fake.intents[1].IntentId || fake.intents[0].IntentId == fake.intents[2].IntentId {
		t.Fatal("retry must reuse intent; new tool execution must use new intent")
	}
	for i, req := range fake.prepares {
		if req.UserId != 7 || req.CheckoutToken != "server-"+fake.intents[i].IntentId {
			t.Fatalf("untrusted identity/token used: %+v", req)
		}
	}
}
func TestAITokenFailureStopsBusinessCall(t *testing.T) {
	fake := &intentRPC{issueError: errors.New("unavailable")}
	_, err := checkoutPrepareHandler(fake)(intentContext("call-1"), intentRequest())
	if err == nil || len(fake.prepares) != 0 {
		t.Fatalf("must not prepare after issuance failure: err=%v calls=%d", err, len(fake.prepares))
	}
}
func TestAIRequiresTrustedExecution(t *testing.T) {
	fake := &intentRPC{}
	_, err := checkoutPrepareHandler(fake)(context.Background(), intentRequest())
	if err == nil || len(fake.prepares) != 0 {
		t.Fatal("missing execution identity must fail before RPC")
	}
}
