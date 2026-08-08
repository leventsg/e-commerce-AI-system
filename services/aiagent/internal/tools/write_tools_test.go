package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"google.golang.org/grpc"
)

func TestUnifiedToolsRegistersLowRiskHandlers(t *testing.T) {
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{
		CartWrite:   &fakeCartWriteRPC{},
		CouponWrite: &fakeCouponWriteRPC{},
	})

	for _, name := range []string{domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCouponClaim} {
		if _, ok := toolHarness.Handler(name); !ok {
			t.Fatalf("write handler %q was not registered", name)
		}
		metadata, err := toolHarness.executor.registry.Metadata(name)
		if err != nil {
			t.Fatalf("metadata %q: %v", name, err)
		}
		if metadata.RequireConfirmation {
			t.Fatalf("%s unexpectedly requires confirmation", name)
		}
		if !metadata.WriteOperation || metadata.TimeoutSeconds != 5 {
			t.Fatalf("metadata %s = %#v, want 5 second write operation", name, metadata)
		}
	}
}

func TestUnifiedToolsCartAddUsesAuthenticatedUserAndRequestedQuantity(t *testing.T) {
	rpc := &fakeCartWriteRPC{}
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{CartWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID:   42,
		ToolName: domain.ToolCartAdd,
		Arguments: map[string]any{
			"user_id":    999,
			"product_id": 12,
			"sku_id":     7,
			"quantity":   3,
		},
	})

	assertWriteToolSuccess(t, event, domain.ToolCartAdd)
	if len(rpc.createReqs) != 1 {
		t.Fatalf("CreateCartItem calls = %d, want 1 (single call with quantity)", len(rpc.createReqs))
	}
	for _, req := range rpc.createReqs {
		if req.UserId != 42 || req.ProductId != 12 || req.Quantity != 3 {
			t.Fatalf("CreateCartItem request = %#v, want trusted user and quantity=3", req)
		}
	}
	data := decodeEventData(t, event)
	if data["product_id"] != float64(12) || data["added_quantity"] != float64(3) {
		t.Fatalf("cart_add data = %#v", data)
	}
}

func TestUnifiedToolsCartSubResolvesOwnedCartItemAndPreservesOne(t *testing.T) {
	rpc := &fakeCartWriteRPC{listResp: &cartsclient.CartItemListResponse{
		Data: []*cartsclient.CartInfoResponse{{Id: 8, UserId: 42, ProductId: 12, Quantity: 4}},
	}}
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{CartWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID:   42,
		ToolName: domain.ToolCartSub,
		Arguments: map[string]any{
			"user_id":      999,
			"cart_item_id": 8,
			"quantity":     2,
		},
	})

	assertWriteToolSuccess(t, event, domain.ToolCartSub)
	if rpc.listReq == nil || rpc.listReq.Id != 42 {
		t.Fatalf("CartItemList request = %#v, want trusted user 42", rpc.listReq)
	}
	if len(rpc.subReqs) != 1 {
		t.Fatalf("SubCartItem calls = %d, want 1 (single call with quantity=2)", len(rpc.subReqs))
	}
	for _, req := range rpc.subReqs {
		if req.UserId != 42 || req.ProductId != 12 || req.Quantity != 2 {
			t.Fatalf("SubCartItem request = %#v, want resolved owned product with quantity=2", req)
		}
	}
	data := decodeEventData(t, event)
	if data["remaining_quantity"] != float64(2) {
		t.Fatalf("cart_sub remaining quantity = %#v, want 2", data["remaining_quantity"])
	}

	rpc.subReqs = nil
	event = toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCartSub,
		Arguments: map[string]any{"cart_item_id": 8, "quantity": 4},
	})
	assertWriteToolFailed(t, event)
	if len(rpc.subReqs) != 0 {
		t.Fatalf("SubCartItem was called when request would delete item: %#v", rpc.subReqs)
	}
}

func TestUnifiedToolsCartSubRejectsCartItemOutsideAuthenticatedUser(t *testing.T) {
	rpc := &fakeCartWriteRPC{listResp: &cartsclient.CartItemListResponse{
		Data: []*cartsclient.CartInfoResponse{{Id: 9, UserId: 42, ProductId: 18, Quantity: 2}},
	}}
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{CartWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCartSub,
		Arguments: map[string]any{"cart_item_id": 8, "quantity": 1},
	})

	assertWriteToolFailed(t, event)
	if len(rpc.subReqs) != 0 {
		t.Fatalf("SubCartItem called for unowned item: %#v", rpc.subReqs)
	}
}

func TestUnifiedToolsCartAddFailureIsReportedAsFailure(t *testing.T) {
	rpc := &fakeCartWriteRPC{createErrAt: 1, createErr: errors.New("cart unavailable")}
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{CartWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCartAdd,
		Arguments: map[string]any{"product_id": 12, "quantity": 3},
	})

	assertWriteToolFailed(t, event)
	if len(rpc.createReqs) != 1 {
		t.Fatalf("CreateCartItem calls = %d, want 1", len(rpc.createReqs))
	}
	if strings.Contains(event.Content, "成功") {
		t.Fatalf("failure content claims success: %q", event.Content)
	}
}

func TestUnifiedToolsCartSubFailureIsReportedAsFailure(t *testing.T) {
	rpc := &fakeCartWriteRPC{
		listResp: &cartsclient.CartItemListResponse{
			Data: []*cartsclient.CartInfoResponse{{Id: 8, UserId: 42, ProductId: 12, Quantity: 4}},
		},		subErrAt: 1,		subErr: errors.New("cart unavailable"),
	}
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{CartWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCartSub,
		Arguments: map[string]any{"cart_item_id": 8, "quantity": 2},
	})

	assertWriteToolFailed(t, event)
	if len(rpc.subReqs) != 1 {
		t.Fatalf("SubCartItem calls = %d, want 1", len(rpc.subReqs))
	}
}

func TestUnifiedToolsCouponClaimUsesAuthenticatedUserAndCompactResult(t *testing.T) {
	rpc := &fakeCouponWriteRPC{resp: &couponsclient.ClaimCouponResp{
		Coupon: &couponsclient.Coupon{Id: "coupon-1", Name: "新人券", Type: coupons.CouponType_COUPON_TYPE_FIXED_AMOUNT},
	}}
	toolHarness := newTestUnifiedWriteHarness(DefaultToolClients{CouponWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCouponClaim,
		Arguments: map[string]any{"coupon_id": "coupon-1", "user_id": 999},
	})

	assertWriteToolSuccess(t, event, domain.ToolCouponClaim)
	if rpc.req == nil || rpc.req.UserId != 42 || rpc.req.CouponId != "coupon-1" {
		t.Fatalf("ClaimCoupon request = %#v", rpc.req)
	}
	data := decodeEventData(t, event)
	if data["coupon_id"] != "coupon-1" || data["name"] != "新人券" {
		t.Fatalf("coupon_claim data = %#v", data)
	}
	if _, ok := data["remaining_count"]; ok {
		t.Fatalf("coupon_claim leaked unrelated coupon fields: %#v", data)
	}
}

func TestUnifiedToolsCouponClaimRejectsBusinessFailureNilAndRPCError(t *testing.T) {
	tests := []struct {
		name string
		rpc  *fakeCouponWriteRPC
	}{
		{name: "business failure", rpc: &fakeCouponWriteRPC{resp: &couponsclient.ClaimCouponResp{StatusCode: 400, StatusMsg: "already claimed"}}},
		{name: "nil response", rpc: &fakeCouponWriteRPC{}},
		{name: "rpc error", rpc: &fakeCouponWriteRPC{err: errors.New("coupon unavailable")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := newTestUnifiedWriteHarness(DefaultToolClients{CouponWrite: tt.rpc}).Execute(context.Background(), ExecuteRequest{
				UserID: 42, ToolName: domain.ToolCouponClaim,
				Arguments: map[string]any{"coupon_id": "coupon-1"},
			})
			assertWriteToolFailed(t, event)
		})
	}
}

func TestUnifiedToolsEinoHandlerRequiresTrustedExecutionContext(t *testing.T) {
	rpc := &fakeCouponWriteRPC{resp: &couponsclient.ClaimCouponResp{
		Coupon: &couponsclient.Coupon{Id: "coupon-1", Name: "新人券"},
	}}
	registry := newTestRegistry(DefaultToolClients{CouponWrite: rpc}, config.ToolTimeoutConfig{})
	NewExecutor(registry)
	tool, err := registry.Tool(domain.ToolCouponClaim)
	if err != nil {
		t.Fatalf("coupon claim tool: %v", err)
	}

	if _, err := tool.InvokableRun(context.Background(), `{"coupon_id":"coupon-1","user_id":999}`); !errors.Is(err, ErrToolExecutionContext) {
		t.Fatalf("InvokableRun error = %v, want ErrToolExecutionContext", err)
	}
	if rpc.req != nil {
		t.Fatalf("ClaimCoupon called without trusted context: %#v", rpc.req)
	}

	ctx := WithToolExecutionContext(context.Background(), ToolExecutionContext{UserID: 42})
	if _, err := tool.InvokableRun(ctx, `{"coupon_id":"coupon-1","user_id":999}`); err != nil {
		t.Fatalf("trusted InvokableRun: %v", err)
	}
	if rpc.req.UserId != 42 {
		t.Fatalf("ClaimCoupon user = %d, want trusted user 42", rpc.req.UserId)
	}
}

func TestUnifiedWriteToolEinoMalformedJSONIsRecorded(t *testing.T) {
	rpc := &fakeCouponWriteRPC{}
	recorder := &capturingToolCallRecorder{}
	registry := newTestRegistry(DefaultToolClients{CouponWrite: rpc}, config.ToolTimeoutConfig{})
	NewExecutor(registry, WithToolCallRecorder(recorder))
	tool, err := registry.Tool(domain.ToolCouponClaim)
	if err != nil {
		t.Fatalf("coupon claim tool: %v", err)
	}
	ctx := WithToolExecutionContext(context.Background(), ToolExecutionContext{
		UserID: 42, ConversationID: "conv-1", MessageID: "msg-1",
	})

	if _, err := tool.InvokableRun(ctx, `{"coupon_id":`); err == nil {
		t.Fatal("InvokableRun malformed JSON returned nil error")
	}
	if len(recorder.records) != 1 {
		t.Fatalf("record count = %d, want 1", len(recorder.records))
	}
	record := recorder.records[0]
	if record.UserID != 42 || record.ToolName != domain.ToolCouponClaim || record.Status != toolStatusFailed {
		t.Fatalf("malformed JSON record = %#v", record)
	}
	if !strings.Contains(record.ErrorMessage, "invalid JSON arguments") {
		t.Fatalf("malformed JSON error = %q", record.ErrorMessage)
	}
	if rpc.req != nil {
		t.Fatalf("ClaimCoupon called for malformed JSON: %#v", rpc.req)
	}
}

func newTestUnifiedWriteHarness(clients DefaultToolClients) *toolTestHarness {
	return newTestToolHarness(clients)
}

func assertWriteToolSuccess(t *testing.T, event domain.AgentEvent, toolName string) {
	t.Helper()
	if event.Status != toolStatusSuccess || event.Tool != toolName {
		t.Fatalf("event = %#v, want successful %s", event, toolName)
	}
}

func assertWriteToolFailed(t *testing.T, event domain.AgentEvent) {
	t.Helper()
	if event.Status != toolStatusFailed {
		t.Fatalf("event = %#v, want failed", event)
	}
}

type fakeCartWriteRPC struct {
	listReq     *cartsclient.UserInfo
	listResp    *cartsclient.CartItemListResponse
	listErr     error
	createReqs  []*cartsclient.CartItemRequest
	createErrAt int
	createErr   error
	subReqs     []*cartsclient.CartItemRequest
	subErrAt    int
	subErr      error
}

func (f *fakeCartWriteRPC) CartItemList(_ context.Context, req *cartsclient.UserInfo, _ ...grpc.CallOption) (*cartsclient.CartItemListResponse, error) {
	f.listReq = req
	return f.listResp, f.listErr
}

func (f *fakeCartWriteRPC) CreateCartItem(_ context.Context, req *cartsclient.CartItemRequest, _ ...grpc.CallOption) (*cartsclient.CreateCartResponse, error) {
	f.createReqs = append(f.createReqs, req)
	if f.createErrAt > 0 && len(f.createReqs) == f.createErrAt {
		return nil, f.createErr
	}
	return &cartsclient.CreateCartResponse{Id: 8}, nil
}

func (f *fakeCartWriteRPC) SubCartItem(_ context.Context, req *cartsclient.CartItemRequest, _ ...grpc.CallOption) (*cartsclient.SubCartResponse, error) {
	f.subReqs = append(f.subReqs, req)
	if f.subErrAt > 0 && len(f.subReqs) == f.subErrAt {
		return nil, f.subErr
	}
	return &cartsclient.SubCartResponse{Id: 8}, nil
}

type fakeCouponWriteRPC struct {
	req  *couponsclient.ClaimCouponReq
	resp *couponsclient.ClaimCouponResp
	err  error
}

func (f *fakeCouponWriteRPC) ClaimCoupon(_ context.Context, req *couponsclient.ClaimCouponReq, _ ...grpc.CallOption) (*couponsclient.ClaimCouponResp, error) {
	f.req = req
	return f.resp, f.err
}
