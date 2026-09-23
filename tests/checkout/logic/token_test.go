//go:build checkout_internal_tests

package logic

import (
	"context"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"
	"testing"
)

func TestIssueRejectsMissingIdentity(t *testing.T) {
	resp, err := NewIssueCheckoutTokenLogic(context.Background(), &svc.ServiceContext{}).IssueCheckoutToken(&checkout.IssueCheckoutTokenReq{IntentId: "intent-1"})
	if err != nil || resp.StatusCode != code.InvalidParams || resp.CheckoutToken != "" {
		t.Fatalf("missing authenticated user must not issue a token: resp=%+v err=%v", resp, err)
	}
}
