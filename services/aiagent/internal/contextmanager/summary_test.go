package contextmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestSummaryManagerSkipsWhenUnsummarizedMessagesBelowThreshold(t *testing.T) {
	store := newFakeSummaryPersistence()
	messages := newSummaryMessages(29, baseTime())
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, &fakeRollingSummarizer{})

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("MaybeRefresh() error = %v", err)
	}
	if result.Created || len(store.saved) != 0 {
		t.Fatalf("summary created = %v saved=%d, want none", result.Created, len(store.saved))
	}
	if len(result.RecentMessages) != 20 || result.RecentMessages[0].MsgId != "m010" || result.RecentMessages[19].MsgId != "m029" {
		t.Fatalf("recent window = %s..%s len=%d, want m010..m029 len=20",
			result.RecentMessages[0].MsgId, result.RecentMessages[len(result.RecentMessages)-1].MsgId, len(result.RecentMessages))
	}
}

func TestSummaryManagerCompactsOldestTenAndKeepsTwentyRecent(t *testing.T) {
	store := newFakeSummaryPersistence()
	old := &domain.ConversationSummary{
		Summary: "用户之前在看数码商品。",
		KeyFacts: map[string]any{
			"budget": "3000",
		},
		OpenTasks:             []string{"等待用户选择品牌"},
		CoveredUntilMessageID: "m000",
		CoveredUntilCreatedAt: baseTime().Add(-time.Minute),
	}
	store.latest = old
	messages := newSummaryMessages(30, baseTime())
	summarizer := &fakeRollingSummarizer{
		response:         `{"summary":"用户之前在看数码商品，并补充偏好轻薄手机。","key_facts":{"budget":"3000","category":"phone"},"open_tasks":["等待用户选择品牌"]}`,
		completionTokens: 123,
	}
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, summarizer)

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("MaybeRefresh() error = %v", err)
	}
	if !result.Created || len(store.saved) != 1 {
		t.Fatalf("summary created=%v saved=%d, want one", result.Created, len(store.saved))
	}
	saved := store.saved[0]
	if saved.CoveredUntilMessageID != "m010" || !saved.CoveredUntilCreatedAt.Equal(baseTime().Add(10*time.Minute)) {
		t.Fatalf("watermark = %s %s, want m010 at +10m", saved.CoveredUntilMessageID, saved.CoveredUntilCreatedAt)
	}
	if saved.Summary != "用户之前在看数码商品，并补充偏好轻薄手机。" ||
		saved.KeyFacts["category"] != "phone" ||
		len(saved.OpenTasks) != 1 ||
		saved.TokenCount != 123 {
		t.Fatalf("saved summary = %+v", saved)
	}
	if len(result.RecentMessages) != 20 || result.RecentMessages[0].MsgId != "m011" || result.RecentMessages[19].MsgId != "m030" {
		t.Fatalf("recent window = %s..%s len=%d, want m011..m030 len=20",
			result.RecentMessages[0].MsgId, result.RecentMessages[len(result.RecentMessages)-1].MsgId, len(result.RecentMessages))
	}
	for _, compressed := range summarizer.messages {
		for _, recent := range result.RecentMessages {
			if compressed.MsgId == recent.MsgId {
				t.Fatalf("message %s appears in both summary input and recent window", compressed.MsgId)
			}
		}
	}
}

func TestSummaryManagerCompactsMultipleRoundsWhenBacklogExceedsTrigger(t *testing.T) {
	store := newFakeSummaryPersistence()
	messages := newSummaryMessages(45, baseTime())
	summarizer := &fakeRollingSummarizer{}
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, summarizer)

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("MaybeRefresh() error = %v", err)
	}
	if !result.Created || len(store.saved) != 2 || summarizer.calls != 2 {
		t.Fatalf("created=%v saved=%d calls=%d, want two rounds", result.Created, len(store.saved), summarizer.calls)
	}
	if store.saved[0].CoveredUntilMessageID != "m010" || store.saved[1].CoveredUntilMessageID != "m020" {
		t.Fatalf("watermarks = %s,%s want m010,m020", store.saved[0].CoveredUntilMessageID, store.saved[1].CoveredUntilMessageID)
	}
	if len(result.RecentMessages) != 20 || result.RecentMessages[0].MsgId != "m026" || result.RecentMessages[19].MsgId != "m045" {
		t.Fatalf("recent window = %s..%s len=%d, want m026..m045 len=20",
			result.RecentMessages[0].MsgId, result.RecentMessages[len(result.RecentMessages)-1].MsgId, len(result.RecentMessages))
	}
}

func TestSummaryManagerStopsAfterMaxRefreshRounds(t *testing.T) {
	store := newFakeSummaryPersistence()
	messages := newSummaryMessages(70, baseTime())
	summarizer := &fakeRollingSummarizer{}
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, summarizer)

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("MaybeRefresh() error = %v", err)
	}
	if !result.Created || len(store.saved) != 3 || summarizer.calls != 3 {
		t.Fatalf("created=%v saved=%d calls=%d, want max three rounds", result.Created, len(store.saved), summarizer.calls)
	}
	if store.saved[2].CoveredUntilMessageID != "m030" {
		t.Fatalf("final watermark = %s, want m030", store.saved[2].CoveredUntilMessageID)
	}
	if len(result.RecentMessages) != 20 || result.RecentMessages[0].MsgId != "m051" || result.RecentMessages[19].MsgId != "m070" {
		t.Fatalf("recent window = %s..%s len=%d, want m051..m070 len=20",
			result.RecentMessages[0].MsgId, result.RecentMessages[len(result.RecentMessages)-1].MsgId, len(result.RecentMessages))
	}
}

func TestSummaryManagerReturnsLastSavedSummaryWhenSecondRoundFails(t *testing.T) {
	store := newFakeSummaryPersistence()
	messages := newSummaryMessages(45, baseTime())
	summarizerErr := errors.New("llm unavailable on second round")
	summarizer := &fakeRollingSummarizer{failOnCall: 2, err: summarizerErr}
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, summarizer)

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if !errors.Is(err, summarizerErr) {
		t.Fatalf("MaybeRefresh() error = %v, want second round error", err)
	}
	if !result.Created || len(store.saved) != 1 || summarizer.calls != 2 {
		t.Fatalf("created=%v saved=%d calls=%d, want first round saved and second attempted", result.Created, len(store.saved), summarizer.calls)
	}
	if result.Summary == nil || result.Summary.CoveredUntilMessageID != "m010" {
		t.Fatalf("result summary = %+v, want first saved summary", result.Summary)
	}
}

func TestSummaryManagerFallsBackToEstimatedTokenCountWhenUsageMissing(t *testing.T) {
	store := newFakeSummaryPersistence()
	messages := newSummaryMessages(30, baseTime())
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, &fakeRollingSummarizer{
		response: `{"summary":"用户想买一台轻薄手机。","key_facts":{"category":"phone"},"open_tasks":[]}`,
	})

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("MaybeRefresh() error = %v", err)
	}
	if !result.Created || len(store.saved) != 1 {
		t.Fatalf("summary created=%v saved=%d, want one", result.Created, len(store.saved))
	}
	if store.saved[0].TokenCount <= 0 || store.saved[0].TokenCount == 123 {
		t.Fatalf("token count = %d, want estimated fallback", store.saved[0].TokenCount)
	}
}

func TestSummaryManagerKeepsPreviousSummaryWhenModelOutputInvalid(t *testing.T) {
	store := newFakeSummaryPersistence()
	store.latest = &domain.ConversationSummary{
		Summary: "旧摘要", CoveredUntilMessageID: "m000", CoveredUntilCreatedAt: baseTime().Add(-time.Minute),
	}
	messages := newSummaryMessages(30, baseTime())
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, &fakeRollingSummarizer{response: `not-json`})

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if err == nil {
		t.Fatal("MaybeRefresh() error = nil, want invalid summary error")
	}
	if result.Created || len(store.saved) != 0 {
		t.Fatalf("summary created=%v saved=%d, want previous summary retained", result.Created, len(store.saved))
	}
	if result.Summary == nil || result.Summary.Summary != "旧摘要" {
		t.Fatalf("result summary = %+v, want previous summary", result.Summary)
	}
	if len(result.RecentMessages) != 20 || result.RecentMessages[0].MsgId != "m011" || result.RecentMessages[19].MsgId != "m030" {
		t.Fatalf("recent window = %s..%s len=%d, want m011..m030 len=20",
			result.RecentMessages[0].MsgId, result.RecentMessages[len(result.RecentMessages)-1].MsgId, len(result.RecentMessages))
	}
}

func TestSummaryManagerDoesNotSaveWhenModelOutputSummaryEmpty(t *testing.T) {
	store := newFakeSummaryPersistence()
	store.latest = &domain.ConversationSummary{
		Summary: "旧摘要", CoveredUntilMessageID: "m000", CoveredUntilCreatedAt: baseTime().Add(-time.Minute),
	}
	messages := newSummaryMessages(30, baseTime())
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, &fakeRollingSummarizer{
		response: `{"summary":"","key_facts":{},"open_tasks":[]}`,
	})

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if !errors.Is(err, ErrInvalidSummaryOutput) {
		t.Fatalf("MaybeRefresh() error = %v, want ErrInvalidSummaryOutput", err)
	}
	if result.Created || len(store.saved) != 0 {
		t.Fatalf("summary created=%v saved=%d, want none", result.Created, len(store.saved))
	}
	if result.Summary == nil || result.Summary.CoveredUntilMessageID != "m000" {
		t.Fatalf("result summary = %+v, want previous watermark retained", result.Summary)
	}
}

func TestSummaryManagerDoesNotAdvanceWatermarkWhenSummarizerFails(t *testing.T) {
	store := newFakeSummaryPersistence()
	store.latest = &domain.ConversationSummary{
		Summary: "旧摘要", CoveredUntilMessageID: "m000", CoveredUntilCreatedAt: baseTime().Add(-time.Minute),
	}
	messages := newSummaryMessages(30, baseTime())
	summarizerErr := errors.New("llm unavailable")
	manager := NewSummaryManager(store, &fakeSummaryMessagesStore{messages: messages}, &fakeRollingSummarizer{err: summarizerErr})

	result, err := manager.MaybeRefresh(context.Background(), SummaryRefreshRequest{UserID: 42, ConversationID: "conv-1"})
	if !errors.Is(err, summarizerErr) {
		t.Fatalf("MaybeRefresh() error = %v, want summarizer error", err)
	}
	if result.Created || len(store.saved) != 0 {
		t.Fatalf("summary created=%v saved=%d, want none", result.Created, len(store.saved))
	}
	if result.Summary == nil || result.Summary.CoveredUntilMessageID != "m000" {
		t.Fatalf("result summary = %+v, want previous watermark retained", result.Summary)
	}
}

func newSummaryMessages(count int, start time.Time) []*aimessages.AiMessages {
	rows := make([]*aimessages.AiMessages, 0, count)
	for i := 1; i <= count; i++ {
		rows = append(rows, &aimessages.AiMessages{
			MsgId:          fmt.Sprintf("m%03d", i),
			UserId:         42,
			ConversationId: "conv-1",
			Role:           map[bool]string{true: domain.ContextRoleUser, false: domain.ContextRoleAssistant}[i%2 == 1],
			Content:        fmt.Sprintf("message-%03d", i),
			CreatedAt:      start.Add(time.Duration(i) * time.Minute),
		})
	}
	return rows
}

type fakeSummaryMessagesStore struct {
	messages []*aimessages.AiMessages
	err      error
}

func (f *fakeSummaryMessagesStore) CountUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return int64(len(f.filterUnsummarized(userID, conversationID, afterCreatedAt, afterMessageID, 0))), nil
}

func (f *fakeSummaryMessagesStore) FindUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.filterUnsummarized(userID, conversationID, afterCreatedAt, afterMessageID, limit), nil
}

func (f *fakeSummaryMessagesStore) FindRecentUnsummarized(ctx context.Context, userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) ([]*aimessages.AiMessages, error) {
	if f.err != nil {
		return nil, f.err
	}
	rows := f.filterUnsummarized(userID, conversationID, afterCreatedAt, afterMessageID, 0)
	return tailMessages(rows, limit), nil
}

func (f *fakeSummaryMessagesStore) filterUnsummarized(userID uint64, conversationID string, afterCreatedAt time.Time, afterMessageID string, limit int) []*aimessages.AiMessages {
	rows := make([]*aimessages.AiMessages, 0, len(f.messages))
	for _, message := range f.messages {
		if message.UserId != userID || message.ConversationId != conversationID {
			continue
		}
		if !afterCreatedAt.IsZero() {
			if message.CreatedAt.Before(afterCreatedAt) || (message.CreatedAt.Equal(afterCreatedAt) && message.MsgId <= afterMessageID) {
				continue
			}
		}
		rows = append(rows, message)
		if limit > 0 && len(rows) >= limit {
			break
		}
	}
	return rows
}

type fakeRollingSummarizer struct {
	response         string
	err              error
	messages         []*aimessages.AiMessages
	previous         *domain.ConversationSummary
	promptTokens     int
	completionTokens int
	totalTokens      int
	calls            int
	failOnCall       int
}

func (f *fakeRollingSummarizer) Summarize(ctx context.Context, req SummarizeRequest) (SummarizeResult, error) {
	f.calls++
	f.previous = req.Previous
	f.messages = append([]*aimessages.AiMessages(nil), req.Messages...)
	if f.err != nil && (f.failOnCall == 0 || f.calls == f.failOnCall) {
		return SummarizeResult{}, f.err
	}
	if strings.TrimSpace(f.response) != "" {
		return SummarizeResult{
			RawOutput:        f.response,
			PromptTokens:     f.promptTokens,
			CompletionTokens: f.completionTokens,
			TotalTokens:      f.totalTokens,
		}, nil
	}
	payload := map[string]any{
		"summary":    "summary",
		"key_facts":  map[string]any{},
		"open_tasks": []string{},
	}
	raw, _ := json.Marshal(payload)
	return SummarizeResult{
		RawOutput:        string(raw),
		PromptTokens:     f.promptTokens,
		CompletionTokens: f.completionTokens,
		TotalTokens:      f.totalTokens,
	}, nil
}

type fakeSummaryPersistence struct {
	latest *domain.ConversationSummary
	saved  []*domain.ConversationSummary
	err    error
}

func newFakeSummaryPersistence() *fakeSummaryPersistence {
	return &fakeSummaryPersistence{}
}

func (f *fakeSummaryPersistence) FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.latest, nil
}

func (f *fakeSummaryPersistence) Save(ctx context.Context, userID uint64, conversationID string, summary *domain.ConversationSummary) error {
	if f.err != nil {
		return f.err
	}
	if summary == nil {
		return errors.New("nil summary")
	}
	f.latest = summary
	f.saved = append(f.saved, summary)
	return nil
}
