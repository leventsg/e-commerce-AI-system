package logout

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
	resp, err := users_client.Logout(context.Background(), &users.LogoutRequest{
		UserId: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("logout success", resp)
	t.Log("logout success", resp)
}
