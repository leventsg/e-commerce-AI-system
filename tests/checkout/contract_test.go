package checkout_test

import (
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"testing"
)

func TestCheckoutIntentContract(t *testing.T) {
	fields := (&checkout.CheckoutReq{}).ProtoReflect().Descriptor().Fields()
	if fields.ByName("checkout_token") == nil {
		t.Fatal("prepare must carry checkout_token so a new purchase of identical items is a distinct intent")
	}
	service := checkout.File_checkout_proto.Services().ByName("CheckoutService")
	if service.Methods().ByName("IssueCheckoutToken") == nil {
		t.Fatal("checkout must issue a user-bound token before prepare")
	}
}
