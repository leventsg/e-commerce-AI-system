package checkout

import (
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
)

var checkoutClient checkout.CheckoutServiceClient

func init() {
	conn, err := grpc.NewClient(fmt.Sprintf("0.0.0.0:%d", biz.CheckoutRpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	checkoutClient = checkout.NewCheckoutServiceClient(conn)
}
