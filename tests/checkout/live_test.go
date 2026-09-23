package checkout_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// Explicitly opt in against a test environment with stock; this reserves two
// checkouts and relies on the normal reservation timeout for release.
func TestLiveCheckoutIntentFlow(t *testing.T) {
	address := os.Getenv("CHECKOUT_TEST_ADDR")
	if address == "" {
		t.Skip("set CHECKOUT_TEST_ADDR, CHECKOUT_TEST_USER and CHECKOUT_TEST_PRODUCT for live integration")
	}
	user, err := strconv.ParseUint(os.Getenv("CHECKOUT_TEST_USER"), 10, 31)
	if err != nil || user == 0 {
		t.Fatal("CHECKOUT_TEST_USER must be a positive test user ID")
	}
	product, err := strconv.ParseInt(os.Getenv("CHECKOUT_TEST_PRODUCT"), 10, 32)
	if err != nil || product <= 0 {
		t.Fatal("CHECKOUT_TEST_PRODUCT must be a stocked test product ID")
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := checkout.NewCheckoutServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var previous string
	for i := 0; i < 2; i++ {
		issued, err := client.IssueCheckoutToken(ctx, &checkout.IssueCheckoutTokenReq{UserId: uint32(user), IntentId: uuid.NewString()})
		if err != nil || issued.GetStatusCode() != 0 || issued.GetCheckoutToken() == "" {
			t.Fatalf("issue: %+v %v", issued, err)
		}
		request := &checkout.CheckoutReq{UserId: uint32(user), CheckoutToken: issued.CheckoutToken, OrderItems: []*checkout.CheckoutReq_OrderItem{{ProductId: int32(product), Quantity: 1}}}
		first, err := client.PrepareCheckout(ctx, request)
		if err != nil || first.GetStatusCode() != 0 || first.GetPreOrderId() == "" {
			t.Fatalf("prepare: %+v %v", first, err)
		}
		repeated, err := client.PrepareCheckout(ctx, request)
		if err != nil || !proto.Equal(first, repeated) {
			t.Fatalf("retry changed outcome: %+v %v", repeated, err)
		}
		if first.PreOrderId == previous {
			t.Fatal("new purchase reused previous preorder")
		}
		previous = first.PreOrderId
	}
}
