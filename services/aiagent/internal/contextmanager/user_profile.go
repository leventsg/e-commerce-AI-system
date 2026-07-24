package contextmanager

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"
)

type UserRPCProfileSource struct {
	users usersclient.Users
}

func NewUserProfileSource(users usersclient.Users) *UserRPCProfileSource {
	return &UserRPCProfileSource{users: users}
}

func (s *UserRPCProfileSource) Load(ctx context.Context, userID uint64) (*domain.UserProfile, error) {
	if s == nil || s.users == nil || userID == 0 {
		return nil, nil
	}
	resp, err := s.users.GetUser(ctx, &usersclient.GetUserRequest{UserId: uint32(userID)})
	if err != nil || resp == nil || resp.StatusCode != uint32(code.Success) {
		return nil, err
	}
	profile := &domain.UserProfile{
		DisplayName: resp.UserName,
		Locale:      "zh-CN",
	}
	if profile.DisplayName == "" {
		return nil, nil
	}
	return profile, nil
}
