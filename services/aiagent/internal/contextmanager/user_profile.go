package contextmanager

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	aiuserprofiles "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_profiles"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const (
	userProfileSourceAIChat = "ai_chat"
	userProfileStatusActive = "active"
)

type userProfileModel interface {
	FindActiveByUser(ctx context.Context, userID uint64) (*aiuserprofiles.AiUserProfiles, error)
	UpsertByUser(ctx context.Context, data *aiuserprofiles.AiUserProfiles) (*aiuserprofiles.AiUserProfiles, error)
}

type UserProfileModelStore struct {
	model userProfileModel
}

func NewUserProfileStore(model userProfileModel) *UserProfileModelStore {
	return &UserProfileModelStore{model: model}
}

func (s *UserProfileModelStore) LoadActive(ctx context.Context, userID uint64) (*domain.UserProfile, error) {
	if s == nil || s.model == nil || userID == 0 {
		return nil, nil
	}
	row, err := s.model.FindActiveByUser(ctx, userID)
	if err != nil || row == nil || !json.Valid([]byte(row.ProfileJson)) {
		return nil, err
	}
	return userProfileFromRow(row), nil
}

func (s *UserProfileModelStore) Upsert(ctx context.Context, profile *domain.UserProfile, userID uint64) (*domain.UserProfile, error) {
	if s == nil || s.model == nil || profile == nil || userID == 0 || !json.Valid(profile.ProfileJSON) {
		return nil, ErrInvalidContextRequest
	}
	saved, err := s.model.UpsertByUser(ctx, &aiuserprofiles.AiUserProfiles{
		Id:          newUserProfileID(),
		UserId:      userID,
		ProfileJson: string(profile.ProfileJSON),
		Version:     profile.Version,
		Source:      userProfileSourceAIChat,
		Status:      userProfileStatusActive,
		LastEventId: profile.LastEventID,
	})
	if err != nil {
		return nil, err
	}
	return userProfileFromRow(saved), nil
}

func userProfileFromRow(row *aiuserprofiles.AiUserProfiles) *domain.UserProfile {
	return &domain.UserProfile{
		ProfileJSON: json.RawMessage(row.ProfileJson),
		Version:     row.Version,
		LastEventID: row.LastEventId,
		UpdatedAt:   row.UpdatedAt,
	}
}

func newUserProfileID() string {
	return "profile_" + uuid.NewString()
}
