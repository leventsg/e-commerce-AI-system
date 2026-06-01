package register

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
	resp, err := users_client.Register(context.Background(), &users.RegisterRequest{
		Email:           "djj126555q@example.com",
		Password:        "password123",
		ConfirmPassword: "password123",
	})
	if err != nil {
		t.Error(err)
	}
	fmt.Println("register success", resp)
	t.Log("register success", resp)
}
