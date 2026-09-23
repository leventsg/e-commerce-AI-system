//go:build checkout_internal_tests

package logic

import (
	"context"
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	rpc "github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"google.golang.org/grpc"
	"testing"
)

type checkoutRPC struct {
	rpc.CheckoutService
	issue   *rpc.IssueCheckoutTokenReq
	prepare *rpc.CheckoutReq
}

func (f *checkoutRPC) IssueCheckoutToken(_ context.Context, in *rpc.IssueCheckoutTokenReq, _ ...grpc.CallOption) (*rpc.IssueCheckoutTokenResp, error) {
	f.issue = in
	return &rpc.IssueCheckoutTokenResp{CheckoutToken: "server-token", ExpiresAt: 99}, nil
}
func (f *checkoutRPC) PrepareCheckout(_ context.Context, in *rpc.CheckoutReq, _ ...grpc.CallOption) (*rpc.CheckoutResp, error) {
	f.prepare = in
	return &rpc.CheckoutResp{PreOrderId: "pre-1", ExpireTime: 99}, nil
}
func TestAPIUsesAuthenticatedUserAndExplicitToken(t *testing.T) {
	fake := &checkoutRPC{}
	service := &svc.ServiceContext{CheckoutRpc: fake}
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(7))
	response, err := NewTokenLogic(ctx, service).Token(&types.CheckoutTokenReq{IntentID: "intent-1"})
	if err != nil || response == nil || response.CheckoutToken != "server-token" || fake.issue == nil || fake.issue.UserId != 7 {
		t.Fatalf("invalid token response: %+v %v", response, err)
	}
	_, err = NewCheckoutLogic(ctx, service).Checkout(&types.CheckoutReq{CheckoutToken: "server-token", OrderItems: []types.CheckoutItemReq{{ProductID: 42, Quantity: 1}}})
	if err != nil || fake.prepare.UserId != 7 || fake.prepare.CheckoutToken != "server-token" {
		t.Fatalf("missing trusted user or token: %+v %v", fake.prepare, err)
	}
}
func TestTokenAPIRejectsUnauthenticatedRequest(t *testing.T) {
	_, err := NewTokenLogic(context.Background(), &svc.ServiceContext{}).Token(&types.CheckoutTokenReq{IntentID: "intent-1"})
	if err == nil {
		t.Fatal("unauthenticated token request must be rejected")
	}
}
