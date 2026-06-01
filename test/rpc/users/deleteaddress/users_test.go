package deleteaddress

import (
	"context"
	"fmt"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var users_client users.UsersClient

func initusers() {

	conn, err := grpc.NewClient(fmt.Sprintf("0.0.0.0:%d", biz.UsersRpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	users_client = users.NewUsersClient(conn)
}

func TestUsersRpc(t *testing.T) {
	initusers()
	//这里可以从token中获取user——id
	resp, err := users_client.DeleteAddress(context.Background(), &users.DeleteAddressRequest{

		AddressId: 2,
		UserId:    1,
	})
	if err != nil {
		fmt.Println("delete address error", err)
		t.Log("delete address error", err)
	}
	fmt.Println("delete success", resp)
	t.Log("deletesuccess", resp)
}
