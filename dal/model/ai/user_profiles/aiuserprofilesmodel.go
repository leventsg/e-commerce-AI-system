package user_profiles

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AiUserProfilesModel = (*customAiUserProfilesModel)(nil)

type (
	// AiUserProfilesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAiUserProfilesModel.
	AiUserProfilesModel interface {
		aiUserProfilesModel
		FindActiveByUser(ctx context.Context, userID uint64) (*AiUserProfiles, error)
		UpsertByUser(ctx context.Context, data *AiUserProfiles) (*AiUserProfiles, error)
	}

	customAiUserProfilesModel struct {
		*defaultAiUserProfilesModel
	}
)

func (m *customAiUserProfilesModel) FindActiveByUser(ctx context.Context, userID uint64) (*AiUserProfiles, error) {
	row, err := m.FindOneByUserId(ctx, userID)
	if errors.Is(err, sqlx.ErrNotFound) || errors.Is(err, sqlc.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row == nil || row.Status != "active" {
		return nil, nil
	}
	return row, nil
}

func (m *customAiUserProfilesModel) UpsertByUser(ctx context.Context, data *AiUserProfiles) (*AiUserProfiles, error) {
	if data == nil || data.UserId == 0 || data.Id == "" {
		return nil, errors.New("invalid ai user profile")
	}
	if err := validateProfileJSON(data.ProfileJson); err != nil {
		return nil, err
	}
	if data.Source == "" {
		data.Source = "ai_chat"
	}
	if data.Status == "" {
		data.Status = "active"
	}

	existing, err := m.FindOneByUserId(ctx, data.UserId)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) && !errors.Is(err, sqlc.ErrNotFound) {
		return nil, err
	}
	if existing == nil || errors.Is(err, sqlx.ErrNotFound) || errors.Is(err, sqlc.ErrNotFound) {
		if data.Version == 0 {
			data.Version = 1
		}
		if _, err := m.Insert(ctx, data); err != nil {
			return nil, err
		}
		return data, nil
	}

	data.Id = existing.Id
	data.UserId = existing.UserId
	data.Version = existing.Version + 1
	if err := m.Update(ctx, data); err != nil {
		return nil, err
	}
	return data, nil
}

func validateProfileJSON(raw string) error {
	if raw == "" || !json.Valid([]byte(raw)) {
		return errors.New("invalid profile json")
	}
	return nil
}

// NewAiUserProfilesModel returns a model for the database table.
func NewAiUserProfilesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) AiUserProfilesModel {
	return &customAiUserProfilesModel{
		defaultAiUserProfilesModel: newAiUserProfilesModel(conn, c, opts...),
	}
}
