package profileextractor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestExtractorSavesExplicitPreferencePatch(t *testing.T) {
	store := &fakeProfileStore{}
	model := &fakeProfileModel{candidate: Candidate{
		ShouldUpdate:       true,
		UpdateType:         UpdateTypeExplicitPreference,
		ProfilePatch:       json.RawMessage(`{"preferences":{"categories":["轻薄手机"]}}`),
		EvidenceMessageIDs: []string{"msg-1"},
		Confidence:         0.95,
	}}
	extractor := NewExtractor(&fakeProfileMessageStore{messages: profileMessages()}, store, nil, model)

	err := extractor.Handle(context.Background(), UpdateEvent{EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}, CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.saved == nil || string(store.saved.ProfileJSON) != `{"preferences":{"categories":["轻薄手机"]}}` || store.saved.LastEventID != "evt-1" {
		t.Fatalf("saved profile = %+v", store.saved)
	}
}

func TestExtractorRejectsLowConfidenceStablePattern(t *testing.T) {
	store := &fakeProfileStore{}
	extractor := NewExtractor(&fakeProfileMessageStore{messages: profileMessages()}, store, nil, &fakeProfileModel{candidate: Candidate{
		ShouldUpdate:       true,
		UpdateType:         UpdateTypeStablePattern,
		ProfilePatch:       json.RawMessage(`{"stable_patterns":["经常看手机"]}`),
		EvidenceMessageIDs: []string{"msg-1"},
		Confidence:         0.8,
	}})

	err := extractor.Handle(context.Background(), UpdateEvent{EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}})
	if !errors.Is(err, ErrRejectedCandidate) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrRejectedCandidate)
	}
	if store.saved != nil {
		t.Fatalf("saved profile = %+v, want nil", store.saved)
	}
}

func TestExtractorAppliesCorrectionAndDeletePatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		update   string
		patch    json.RawMessage
		wantJSON string
	}{
		{
			name:     "correction",
			update:   UpdateTypeCorrection,
			patch:    json.RawMessage(`{"preferences":{"brands":["白色"]},"corrections":["颜色偏好已纠正"]}`),
			wantJSON: `{"corrections":["颜色偏好已纠正"],"preferences":{"brands":["白色"],"categories":["手机"]}}`,
		},
		{
			name:     "delete",
			update:   UpdateTypeDeleteOrForget,
			patch:    json.RawMessage(`{"preferences":{"brands":null},"deleted_or_disabled":["brands"]}`),
			wantJSON: `{"deleted_or_disabled":["brands"],"preferences":{"categories":["手机"]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeProfileStore{profile: &domain.UserProfile{ProfileJSON: json.RawMessage(`{"preferences":{"categories":["手机"],"brands":["黑色"]}}`)}}
			extractor := NewExtractor(&fakeProfileMessageStore{messages: profileMessages()}, store, nil, &fakeProfileModel{candidate: Candidate{
				ShouldUpdate:       true,
				UpdateType:         tc.update,
				ProfilePatch:       tc.patch,
				EvidenceMessageIDs: []string{"msg-1"},
				Confidence:         0.95,
			}})

			if err := extractor.Handle(context.Background(), UpdateEvent{EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}}); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if string(store.saved.ProfileJSON) != tc.wantJSON {
				t.Fatalf("profile json = %s, want %s", store.saved.ProfileJSON, tc.wantJSON)
			}
		})
	}
}

func TestExtractorRejectsSensitiveProfilePatch(t *testing.T) {
	store := &fakeProfileStore{}
	extractor := NewExtractor(&fakeProfileMessageStore{messages: profileMessages()}, store, nil, &fakeProfileModel{candidate: Candidate{
		ShouldUpdate:       true,
		UpdateType:         UpdateTypeExplicitPreference,
		ProfilePatch:       json.RawMessage(`{"preferences":{"payment":["银行卡 6222000000000000"]}}`),
		EvidenceMessageIDs: []string{"msg-1"},
		Confidence:         0.95,
	}})

	err := extractor.Handle(context.Background(), UpdateEvent{EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}})
	if !errors.Is(err, ErrRejectedCandidate) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrRejectedCandidate)
	}
	if store.saved != nil {
		t.Fatalf("saved profile = %+v, want nil", store.saved)
	}
}

func TestParseCandidateRejectsNonJSONObjectOutput(t *testing.T) {
	if _, err := ParseCandidate(`{"should_update":true,"update_type":"explicit_preference","profile_patch":{"preferences":{"categories":["手机"]}},"evidence_message_ids":["msg-1"],"confidence":0.9}`); err != nil {
		t.Fatalf("ParseCandidate() error = %v", err)
	}
	if _, err := ParseCandidate("```json\n{}\n```"); err == nil {
		t.Fatal("ParseCandidate() error = nil, want markdown rejection")
	}
}

func profileMessages() []*aimessages.AiMessages {
	return []*aimessages.AiMessages{{MsgId: "msg-1", UserId: 42, ConversationId: "conv-1", Role: domain.ContextRoleUser, Content: "以后推荐轻薄手机"}}
}

type fakeProfileMessageStore struct {
	messages []*aimessages.AiMessages
}

func (f *fakeProfileMessageStore) FindMessagesByIDs(context.Context, uint64, string, []string) ([]*aimessages.AiMessages, error) {
	return f.messages, nil
}

type fakeProfileStore struct {
	profile *domain.UserProfile
	saved   *domain.UserProfile
}

func (f *fakeProfileStore) LoadActive(context.Context, uint64) (*domain.UserProfile, error) {
	return f.profile, nil
}

func (f *fakeProfileStore) Upsert(_ context.Context, profile *domain.UserProfile, _ uint64) (*domain.UserProfile, error) {
	f.saved = profile
	return profile, nil
}

type fakeProfileModel struct {
	candidate Candidate
}

func (f *fakeProfileModel) Extract(context.Context, ExtractRequest) (Candidate, error) {
	return f.candidate, nil
}
