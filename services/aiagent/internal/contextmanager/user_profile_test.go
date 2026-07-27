package contextmanager

import (
	"context"
	"testing"
	"time"

	aiuserprofiles "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_profiles"
)

func TestUserProfileStoreLoadsActiveProfileJSON(t *testing.T) {
	store := NewUserProfileStore(&fakeUserProfileModel{row: &aiuserprofiles.AiUserProfiles{
		UserId: 42, ProfileJson: `{"preferences":{"categories":["手机"]}}`, Version: 2, LastEventId: "evt-1",
		Status: "active", UpdatedAt: baseTime(),
	}})

	profile, err := store.LoadActive(context.Background(), 42)
	if err != nil {
		t.Fatalf("LoadActive() error = %v", err)
	}
	if profile == nil || string(profile.ProfileJSON) != `{"preferences":{"categories":["手机"]}}` ||
		profile.Version != 2 || profile.LastEventID != "evt-1" || !profile.UpdatedAt.Equal(baseTime()) {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestUserProfileStoreSkipsInvalidOrInactiveProfile(t *testing.T) {
	for _, row := range []*aiuserprofiles.AiUserProfiles{
		{UserId: 42, ProfileJson: `{"preferences":`, Status: "active"},
		nil,
	} {
		store := NewUserProfileStore(&fakeUserProfileModel{row: row})
		profile, err := store.LoadActive(context.Background(), 42)
		if err != nil {
			t.Fatalf("LoadActive() error = %v", err)
		}
		if profile != nil {
			t.Fatalf("profile = %+v, want nil", profile)
		}
	}
}

type fakeUserProfileModel struct {
	row    *aiuserprofiles.AiUserProfiles
	saved  *aiuserprofiles.AiUserProfiles
	userID uint64
	err    error
}

func (f *fakeUserProfileModel) FindActiveByUser(_ context.Context, userID uint64) (*aiuserprofiles.AiUserProfiles, error) {
	f.userID = userID
	return f.row, f.err
}

func (f *fakeUserProfileModel) UpsertByUser(_ context.Context, data *aiuserprofiles.AiUserProfiles) (*aiuserprofiles.AiUserProfiles, error) {
	f.saved = data
	if data.UpdatedAt.IsZero() {
		data.UpdatedAt = time.Now()
	}
	return data, f.err
}
