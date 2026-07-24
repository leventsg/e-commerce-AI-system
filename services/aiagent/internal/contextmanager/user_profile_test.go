package contextmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"
	"google.golang.org/grpc"
)

func TestUserProfileSourceLoadsAllowlistedFields(t *testing.T) {
	users := &fakeUserRPC{resp: &usersclient.GetUserResponse{StatusCode: uint32(code.Success), UserName: "小明", Email: "secret@example.com"}}
	source := NewUserProfileSource(users)

	profile, err := source.Load(context.Background(), 42)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if profile == nil || profile.DisplayName != "小明" || profile.Locale != "zh-CN" {
		t.Fatalf("profile = %+v", profile)
	}
	if users.userID != 42 {
		t.Fatalf("GetUser user_id = %d, want trusted user id 42", users.userID)
	}
}

func TestUserProfileSourceSkipsWhenUsersRPCFails(t *testing.T) {
	source := NewUserProfileSource(&fakeUserRPC{err: errors.New("users unavailable")})
	profile, err := source.Load(context.Background(), 42)
	if err == nil {
		t.Fatal("Load() error = nil, want RPC error for caller to degrade")
	}
	if profile != nil {
		t.Fatalf("profile = %+v, want nil", profile)
	}
}

type fakeUserRPC struct {
	usersclient.Users
	resp   *usersclient.GetUserResponse
	err    error
	userID uint32
}

func (f *fakeUserRPC) GetUser(ctx context.Context, in *usersclient.GetUserRequest, opts ...grpc.CallOption) (*usersclient.GetUserResponse, error) {
	f.userID = in.UserId
	return f.resp, f.err
}
