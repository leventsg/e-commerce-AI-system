package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"google.golang.org/grpc"
)

func TestCheckoutPrepareInjectsUserAndConvertsOrderItems(t *testing.T) {
	rpc := &fakeCheckoutWriteRPC{resp: &checkoutservice.CheckoutResp{
		PreOrderId: "pre-1", ExpireTime: 12345, PayMethod: []int64{1, 2},
	}}
	toolHarness := newTestToolHarness(DefaultToolClients{CheckoutWrite: rpc})

	event := toolHarness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCheckoutPrepare,
		Arguments: map[string]any{
			"user_id":   999,
			"coupon_id": "coupon-1",
			"order_items": []any{
				map[string]any{"product_id": float64(11), "quantity": float64(2)},
				map[string]any{"product_id": float64(12), "quantity": float64(1)},
			},
		},
	})

	assertWriteToolSuccess(t, event, domain.ToolCheckoutPrepare)
	if rpc.req == nil || rpc.req.UserId != 42 || rpc.req.CouponId != "coupon-1" {
		t.Fatalf("PrepareCheckout request = %#v", rpc.req)
	}
	if len(rpc.req.OrderItems) != 2 || rpc.req.OrderItems[0].ProductId != 11 || rpc.req.OrderItems[0].Quantity != 2 {
		t.Fatalf("PrepareCheckout items = %#v", rpc.req.OrderItems)
	}
	data := decodeEventData(t, event)
	if data["pre_order_id"] != "pre-1" || data["expire_time"] != float64(12345) {
		t.Fatalf("checkout_prepare data = %#v", data)
	}
}

func TestCheckoutPrepareRejectsInvalidItemsAndRPCFailures(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		rpc  *fakeCheckoutWriteRPC
	}{
		{name: "missing items", args: map[string]any{}, rpc: &fakeCheckoutWriteRPC{}},
		{name: "invalid quantity", args: map[string]any{"order_items": []any{map[string]any{"product_id": 1, "quantity": 0}}}, rpc: &fakeCheckoutWriteRPC{}},
		{name: "rpc error", args: validCheckoutArguments(), rpc: &fakeCheckoutWriteRPC{err: errors.New("checkout unavailable")}},
		{name: "nil response", args: validCheckoutArguments(), rpc: &fakeCheckoutWriteRPC{}},
		{name: "business failure", args: validCheckoutArguments(), rpc: &fakeCheckoutWriteRPC{resp: &checkoutservice.CheckoutResp{StatusCode: 400, StatusMsg: "failed"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := newTestToolHarness(DefaultToolClients{CheckoutWrite: tt.rpc}).Execute(
				context.Background(), ExecuteRequest{UserID: 42, ToolName: domain.ToolCheckoutPrepare, Arguments: tt.args},
			)
			assertWriteToolFailed(t, event)
		})
	}
}

func validCheckoutArguments() map[string]any {
	return map[string]any{"order_items": []any{map[string]any{"product_id": 1, "quantity": 1}}}
}

type fakeCheckoutWriteRPC struct {
	req  *checkoutservice.CheckoutReq
	resp *checkoutservice.CheckoutResp
	err  error
}

func (f *fakeCheckoutWriteRPC) PrepareCheckout(_ context.Context, req *checkoutservice.CheckoutReq, _ ...grpc.CallOption) (*checkoutservice.CheckoutResp, error) {
	f.req = req
	return f.resp, f.err
}
