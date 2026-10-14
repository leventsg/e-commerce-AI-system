package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestHistoryMessagesTokenWindow(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []*aimessages.AiMessages{
		{MsgId: "m1", Role: domain.ContextRoleUser, Content: "第一", CreatedAt: base},
		{MsgId: "m2", Role: domain.ContextRoleAssistant, Content: "第二", CreatedAt: base.Add(time.Minute)},
		{MsgId: "m3", Role: domain.ContextRoleUser, Content: "第三", CreatedAt: base.Add(2 * time.Minute)},
		{MsgId: "m4", Role: domain.ContextRoleAssistant, Content: "第四", CreatedAt: base.Add(3 * time.Minute)},
		{MsgId: "m5", Role: domain.ContextRoleUser, Content: "第五", CreatedAt: base.Add(4 * time.Minute)},
	}
	budget := domain.TokenBudget{RecentWindowTokens: 6, MinRecentMessages: 2}
	got := historyMessages(rows, "", nil, budget, domain.HybridEstimator{})
	// 每条 2 token，预算 6 → 保留最近 3 条（m3/m4/m5）。
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Content != "第三" || got[len(got)-1].Content != "第五" {
		t.Fatalf("window = %+v", got)
	}
}

func TestHistoryMessagesLongMessageExhaustsBudget(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []*aimessages.AiMessages{
		{MsgId: "m1", Role: domain.ContextRoleUser, Content: "旧", CreatedAt: base},
		{MsgId: "m2", Role: domain.ContextRoleAssistant, Content: strings.Repeat("长", 50), CreatedAt: base.Add(time.Minute)},
		{MsgId: "m3", Role: domain.ContextRoleUser, Content: "新", CreatedAt: base.Add(2 * time.Minute)},
	}
	budget := domain.TokenBudget{RecentWindowTokens: 10, MinRecentMessages: 1}
	got := historyMessages(rows, "", nil, budget, domain.HybridEstimator{})
	// 长消息（50 token）更快耗尽预算，只保留最近 1 条。
	if len(got) != 1 || got[0].Content != "新" {
		t.Fatalf("long message should exhaust budget, window = %+v", got)
	}
}

func TestHistoryMessagesKeepsMinRecentWhenBudgetExhausted(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []*aimessages.AiMessages{
		{MsgId: "m1", Role: domain.ContextRoleUser, Content: strings.Repeat("长", 50), CreatedAt: base},
		{MsgId: "m2", Role: domain.ContextRoleAssistant, Content: strings.Repeat("长", 50), CreatedAt: base.Add(time.Minute)},
	}
	budget := domain.TokenBudget{RecentWindowTokens: 10, MinRecentMessages: 2}
	got := historyMessages(rows, "", nil, budget, domain.HybridEstimator{})
	// 预算耗尽仍保底保留最近 2 条，避免窗口为空。
	if len(got) != 2 {
		t.Fatalf("min recent not honored, len = %d", len(got))
	}
}

type fakeCompressor struct {
	ids   []string
	err   error
	calls int
}

func (f *fakeCompressor) CompressIfNeeded(_ context.Context, _ uint64, _ string) ([]string, error) {
	f.calls++
	return f.ids, f.err
}

func TestRetrieveCompressesWhenNeededAndPublishes(t *testing.T) {
	compressor := &fakeCompressor{ids: []string{"m1", "m2"}}
	done := make(chan []string, 1)
	provider := NewCustomerServiceProvider(CustomerServiceProviderConfig{
		Compressor: compressor,
		OnCompressed: func(_ context.Context, _ uint64, _ string, ids []string) {
			done <- ids
		},
		Budget: domain.TokenBudget{},
	})
	result, err := provider.Retrieve(context.Background(), &RetrieveRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("Retrieve error = %v", err)
	}
	if compressor.calls != 1 {
		t.Fatalf("compressor calls = %d, want 1", compressor.calls)
	}
	if result.Metadata["context_compressed"] != true {
		t.Fatalf("metadata = %+v", result.Metadata)
	}
	select {
	case ids := <-done:
		if len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" {
			t.Fatalf("published ids = %v", ids)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("compress callback not invoked")
	}
}

func TestRetrieveSkipsCompressionWhenNotNeeded(t *testing.T) {
	compressor := &fakeCompressor{ids: nil}
	provider := NewCustomerServiceProvider(CustomerServiceProviderConfig{
		Compressor: compressor,
		Budget:     domain.TokenBudget{},
	})
	result, err := provider.Retrieve(context.Background(), &RetrieveRequest{UserID: 42, ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("Retrieve error = %v", err)
	}
	if compressor.calls != 1 {
		t.Fatalf("compressor should be consulted every retrieve, calls = %d", compressor.calls)
	}
	if _, ok := result.Metadata["context_compressed"]; ok {
		t.Fatalf("should not mark compressed when no ids returned: %+v", result.Metadata)
	}
}
