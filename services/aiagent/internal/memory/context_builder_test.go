package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type fakeMessageStore struct{ rows []*aimessages.AiMessages }

func (f *fakeMessageStore) FindRecent(_ context.Context, _ uint64, _ string, _ int) ([]*aimessages.AiMessages, error) {
	return f.rows, nil
}

type fakeSummaryStore struct{ summary *domain.ConversationSummary }

func (f *fakeSummaryStore) FindLatest(_ context.Context, _ uint64, _ string) (*domain.ConversationSummary, error) {
	return f.summary, nil
}

type fakeProfileStore struct{ profile *domain.UserProfile }

func (f *fakeProfileStore) LoadActive(_ context.Context, _ uint64) (*domain.UserProfile, error) {
	return f.profile, nil
}

type fakeEventStore struct{ recent, relevant []Event }

func (f *fakeEventStore) ListRecentUserMemoryEvents(_ context.Context, _ uint64, _ int) ([]Event, error) {
	return f.recent, nil
}

func (f *fakeEventStore) SearchUserMemoryEvents(_ context.Context, _ UserMemoryEventQuery) ([]Event, error) {
	return f.relevant, nil
}

type fakeToolStore struct{}

func (f *fakeToolStore) FindLatestToolResult(_ context.Context, _ uint64, _ string) (*aitoolcalls.AiToolCalls, error) {
	return nil, nil
}

func (f *fakeToolStore) FindRecentToolCall(_ context.Context, _ uint64, _ string, _ int) ([]*aitoolcalls.AiToolCalls, error) {
	return nil, nil
}

type fakeTaskStore struct{}

func (f *fakeTaskStore) FindActive(_ context.Context, _ uint64, _, _ string) (*domain.TaskState, error) {
	return nil, nil
}

func testProvider() *CustomerServiceProvider {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	profile := &domain.UserProfile{ProfileJSON: []byte(`{"preference":"轻便"}`)}
	summary := &domain.ConversationSummary{Summary: "用户想买手机"}
	rows := []*aimessages.AiMessages{
		{MsgId: "m1", Role: domain.ContextRoleUser, Content: "想买手机", CreatedAt: base},
		{MsgId: "m2", Role: domain.ContextRoleAssistant, Content: "预算多少", CreatedAt: base.Add(time.Minute)},
	}
	recentEvents := []Event{{UserID: 42, Type: "milestone", EventDate: "2026-01-01", Summary: "完成下单"}}
	return NewCustomerServiceProvider(CustomerServiceProviderConfig{
		Messages:  &fakeMessageStore{rows: rows},
		Summaries: &fakeSummaryStore{summary: summary},
		Profiles:  &fakeProfileStore{profile: profile},
		Events:    &fakeEventStore{recent: recentEvents},
		Tools:     &fakeToolStore{},
		TaskStates: &fakeTaskStore{},
		Budget:    domain.TokenBudget{},
	})
}

func joinAllContents(msgs []domain.ContextMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func TestRetrieveSplitsStableFromVolatile(t *testing.T) {
	p := testProvider()
	req := &RetrieveRequest{UserID: 42, ConversationID: "conv-1", Messages: []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "帮我推荐手机"}}}
	result, err := p.Retrieve(context.Background(), req)
	if err != nil {
		t.Fatalf("Retrieve error = %v", err)
	}
	stable := joinAllContents(result.SystemMessages)
	volatile := joinAllContents(result.ContextMessages)
	// 稳定前缀包含画像与长期事件。
	if !strings.Contains(stable, "user_profile") {
		t.Fatalf("stable prefix missing user_profile: %q", stable)
	}
	if !strings.Contains(stable, "user_memory_recent_events") {
		t.Fatalf("stable prefix missing events: %q", stable)
	}
	// 稳定内容不再混入易变后缀。
	if strings.Contains(volatile, "user_profile") || strings.Contains(volatile, "user_memory_recent_events") {
		t.Fatalf("volatile suffix should not contain stable blocks: %q", volatile)
	}
	// 易变后缀包含摘要与当前时间。
	if !strings.Contains(volatile, "conversation_context") {
		t.Fatalf("volatile suffix missing summary: %q", volatile)
	}
	if !strings.Contains(volatile, "current_time") {
		t.Fatalf("volatile suffix missing current_time: %q", volatile)
	}
	// 历史消息在 HistoryMessages 而非 ContextMessages。
	if len(result.HistoryMessages) != 2 {
		t.Fatalf("history len = %d, want 2", len(result.HistoryMessages))
	}
}

func TestRetrieveSkipStableOmitsSystemMessages(t *testing.T) {
	p := testProvider()
	req := &RetrieveRequest{UserID: 42, ConversationID: "conv-1", SkipStable: true, Messages: []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "x"}}}
	result, err := p.Retrieve(context.Background(), req)
	if err != nil {
		t.Fatalf("Retrieve error = %v", err)
	}
	if len(result.SystemMessages) != 0 {
		t.Fatalf("SkipStable should omit SystemMessages, got %d", len(result.SystemMessages))
	}
	if len(result.ContextMessages) == 0 {
		t.Fatal("SkipStable should still refresh volatile ContextMessages")
	}
}

func TestPrepareStablePlacesProfileAfterEvents(t *testing.T) {
	p := testProvider()
	builder := p.context
	req := &RetrieveRequest{UserID: 42, AgentName: "supervisor_agent", Messages: []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "帮我推荐手机"}}}
	stable := builder.PrepareStable(context.Background(), req)
	joined := joinAllContents(stable)
	// 事件在前、画像在后（保持稳定前缀内相对顺序稳定）。
	if strings.Index(joined, "user_memory_recent_events") > strings.Index(joined, "user_profile") {
		t.Fatalf("events should precede profile in stable prefix: %q", joined)
	}
}

func TestRefreshVolatileIncludesRAG(t *testing.T) {
	p := testProvider()
	rag := []domain.ContextMessage{{Role: domain.ContextRoleSystem, Content: "【知识库参考】\n文档：退换货政策"}}
	req := &RetrieveRequest{UserID: 42, ConversationID: "conv-1", RAGContext: rag}
	volatile, err := p.context.RefreshVolatile(context.Background(), req)
	if err != nil {
		t.Fatalf("RefreshVolatile error = %v", err)
	}
	if !strings.Contains(joinAllContents(volatile.ContextMessages), "退换货政策") {
		t.Fatalf("RAG context missing from volatile suffix: %q", joinAllContents(volatile.ContextMessages))
	}
}
