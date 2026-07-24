package contextmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestMemoryPolicySavesUpdatesDeletesAndExpiresExplicitMemory(t *testing.T) {
	now := baseTime()
	store := newFakeMemoryPersistence()
	policy := NewMemoryPolicy(store, WithMemoryPolicyClock(func() time.Time { return now }))

	saved, err := policy.SaveExplicit(context.Background(), MemoryCommand{
		UserID: 42, MemoryKey: "preference:brand", MemoryType: domain.MemoryTypePreference,
		Content: "用户偏好品牌 A", SourceMessageID: "m1",
	})
	if err != nil {
		t.Fatalf("SaveExplicit() error = %v", err)
	}
	if saved.Status != domain.MemoryStatusActive || saved.Source != domain.MemorySourceExplicit ||
		saved.Confidence != 1 || saved.LastConfirmedAt == nil || !saved.LastConfirmedAt.Equal(now) {
		t.Fatalf("saved memory = %+v", saved)
	}

	updated, err := policy.SaveExplicit(context.Background(), MemoryCommand{
		UserID: 42, MemoryKey: "preference:brand", MemoryType: domain.MemoryTypePreference,
		Content: "用户偏好品牌 B", SourceMessageID: "m2",
	})
	if err != nil {
		t.Fatalf("SaveExplicit update error = %v", err)
	}
	if updated.Id != saved.Id || updated.Content != "用户偏好品牌 B" || updated.SourceMessageID != "m2" {
		t.Fatalf("updated memory = %+v, original = %+v", updated, saved)
	}

	if err := policy.Delete(context.Background(), 42, "preference:brand"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	deleted := store.byKey["preference:brand"]
	if deleted.Status != domain.MemoryStatusDeleted {
		t.Fatalf("deleted status = %q, want deleted", deleted.Status)
	}

	expiring, err := policy.SaveExplicit(context.Background(), MemoryCommand{
		UserID: 42, MemoryKey: "price:phone", MemoryType: domain.MemoryTypePrice,
		Content: "手机预算 3000 元以内", SourceMessageID: "m3", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("SaveExplicit expiring error = %v", err)
	}
	if expiring.ExpiresAt == nil || !expiring.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires_at = %+v, want +1h", expiring.ExpiresAt)
	}

	now = now.Add(2 * time.Hour)
	expired, err := policy.ExpireDue(context.Background(), 42)
	if err != nil {
		t.Fatalf("ExpireDue() error = %v", err)
	}
	if expired != 1 || store.byKey["price:phone"].Status != domain.MemoryStatusExpired {
		t.Fatalf("expired=%d memory=%+v", expired, store.byKey["price:phone"])
	}
}

func TestMemoryPolicyOnlyAcceptsControlledInferredMemory(t *testing.T) {
	now := baseTime()
	store := newFakeMemoryPersistence()
	policy := NewMemoryPolicy(store, WithMemoryPolicyClock(func() time.Time { return now }))

	accepted, err := policy.SaveInferred(context.Background(), MemoryCandidate{
		UserID: 42, MemoryKey: "preference:category", MemoryType: domain.MemoryTypePreference,
		Content: "用户多次关注轻薄手机", Confidence: 0.91, SourceMessageID: "m10", TTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SaveInferred accepted error = %v", err)
	}
	if accepted == nil || accepted.Source != domain.MemorySourceInferred ||
		accepted.Status != domain.MemoryStatusActive ||
		accepted.ExpiresAt == nil ||
		!accepted.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("accepted memory = %+v", accepted)
	}

	rejected := []MemoryCandidate{
		{UserID: 42, MemoryKey: "low-confidence", MemoryType: domain.MemoryTypePreference, Content: "可能喜欢红色", Confidence: 0.84, SourceMessageID: "m11", TTL: time.Hour},
		{UserID: 42, MemoryKey: "missing-source", MemoryType: domain.MemoryTypePreference, Content: "喜欢手机", Confidence: 0.9, TTL: time.Hour},
		{UserID: 42, MemoryKey: "missing-ttl", MemoryType: domain.MemoryTypePreference, Content: "喜欢手机", Confidence: 0.9, SourceMessageID: "m12"},
		{UserID: 42, MemoryKey: "sensitive-token", MemoryType: domain.MemoryTypeInstruction, Content: "token=secret", Confidence: 0.9, SourceMessageID: "m13", TTL: time.Hour},
		{UserID: 42, MemoryKey: "address", MemoryType: domain.MemoryTypeProfileFact, Content: "完整地址是北京市朝阳区某某路 1 号", Confidence: 0.9, SourceMessageID: "m14", TTL: time.Hour},
	}
	for _, candidate := range rejected {
		if memory, err := policy.SaveInferred(context.Background(), candidate); err == nil || memory != nil {
			t.Fatalf("SaveInferred(%s) = %+v, %v; want rejection", candidate.MemoryKey, memory, err)
		}
	}
	if len(store.byKey) != 1 {
		keys := make([]string, 0, len(store.byKey))
		for key := range store.byKey {
			keys = append(keys, key)
		}
		t.Fatalf("stored keys = %s, want only accepted candidate", strings.Join(keys, ","))
	}
}

type fakeMemoryPersistence struct {
	byKey map[string]*domain.UserMemory
}

func newFakeMemoryPersistence() *fakeMemoryPersistence {
	return &fakeMemoryPersistence{byKey: make(map[string]*domain.UserMemory)}
}

func (f *fakeMemoryPersistence) FindByKey(ctx context.Context, userID uint64, key string) (*domain.UserMemory, error) {
	memory := f.byKey[key]
	if memory == nil || memory.UserID != userID {
		return nil, nil
	}
	copy := *memory
	return &copy, nil
}

func (f *fakeMemoryPersistence) Upsert(ctx context.Context, memory *domain.UserMemory) (*domain.UserMemory, error) {
	copy := *memory
	if copy.Id == "" {
		copy.Id = "mem-" + copy.Key
	}
	f.byKey[copy.Key] = &copy
	return &copy, nil
}

func (f *fakeMemoryPersistence) ListActive(ctx context.Context, userID uint64, limit int, now time.Time) ([]domain.UserMemory, error) {
	result := make([]domain.UserMemory, 0, len(f.byKey))
	for _, memory := range f.byKey {
		if memory.UserID != userID || memory.Status != domain.MemoryStatusActive {
			continue
		}
		if memory.ExpiresAt != nil && !memory.ExpiresAt.After(now) {
			continue
		}
		result = append(result, *memory)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (f *fakeMemoryPersistence) ExpireDue(ctx context.Context, userID uint64, now time.Time) (int, error) {
	count := 0
	for _, memory := range f.byKey {
		if memory.UserID == userID && memory.Status == domain.MemoryStatusActive &&
			memory.ExpiresAt != nil && !memory.ExpiresAt.After(now) {
			memory.Status = domain.MemoryStatusExpired
			count++
		}
	}
	return count, nil
}
