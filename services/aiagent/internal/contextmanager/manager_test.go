package contextmanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestManagerBuildsAgentContextWithTwentyRecentMessagesAndToolReferences(t *testing.T) {
	rows := make([]*aimessages.AiMessages, 0, 23)
	for i := 1; i <= 22; i++ {
		rows = append(rows, contextMessage(
			contextMessageID(i),
			map[bool]string{true: "user", false: "assistant"}[i%2 == 1],
			"recent-"+contextMessageID(i),
			baseTime().Add(time.Duration(i)*time.Minute),
		))
	}
	rows = append(rows, contextMessage("current", "user", "继续聊", baseTime().Add(23*time.Minute)))

	messages := &fakeContextMessageStore{messages: rows}
	summaries := &fakeSummaryStore{summary: &domain.ConversationSummary{
		Summary: "更早的会话摘要", CoveredUntilMessageID: "summary-10", CoveredUntilCreatedAt: baseTime(),
	}}
	taskStates := &fakeTaskStateStore{state: &domain.TaskState{Goal: "完成购物选择", PendingConfirmationID: "confirm-1"}}
	profiles := &fakeUserProfileStore{profile: &domain.UserProfile{ProfileJSON: json.RawMessage(`{"preferences":{"categories":["手机"],"brands":["品牌A"]},"evidence":["m2"]}`), Version: 3, LastEventID: "evt-1"}}
	tools := &fakeToolContextStore{
		latest: &aitoolcalls.AiToolCalls{
			ToolCallId: "call-latest", ToolName: domain.ToolCartList, Status: "success",
			Result: `{"items":[{"cart_item_id":7}]}`,
		},
		recent: []*aitoolcalls.AiToolCalls{
			{ToolCallId: "call-latest", ToolName: domain.ToolCartList, Status: "success", Result: `{"items":[{"cart_item_id":7}]}`},
			{ToolCallId: "call-old", ToolName: domain.ToolProductRecommend, Status: "success", Result: `{"products":[{"product_id":12}]}`},
		},
	}
	manager := NewManager(messages, tools,
		WithSummaryStore(summaries),
		WithTaskStateStore(taskStates),
		WithUserProfileStore(profiles),
	)

	result, err := manager.Build(context.Background(), domain.BuildContextRequest{
		UserID: 42, ConversationID: "conv-1", RunID: "run-1",
		Mode: domain.AgentContextMode, CurrentMessageID: "current", CurrentInput: strings.Repeat("继续聊很长的内容", 2000),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	joined := joinContextContents(result.Messages)
	for _, want := range []string{"更早的会话摘要", "recent-m3", "recent-m22", "call-latest", "cart_item_id", "call-old", "完成购物选择", `"categories":["手机"]`, `"brands":["品牌A"]`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("AgentContext missing %q", want)
		}
	}
	if strings.Contains(joined, "预算 3000 元") {
		t.Fatalf("AgentContext should not include removed user memories: %s", joined)
	}
	for _, forbidden := range []string{"recent-m1", "recent-m2"} {
		if countContextContent(result.Messages, forbidden) != 0 {
			t.Fatalf("AgentContext retained message outside latest 20: %q", forbidden)
		}
	}
	if result.SummaryCoveredMessageID != "summary-10" ||
		!result.SummaryCoveredUntilCreatedAt.Equal(baseTime()) ||
		result.RecentMessageStartID != "m3" ||
		result.RecentMessageEndID != "m22" ||
		result.LatestToolCallID != "call-latest" ||
		result.RecentToolCallCount != 1 {
		t.Fatalf("build metadata = %+v", result)
	}
	if result.EstimatedInputTokens <= 0 {
		t.Fatalf("EstimatedInputTokens = %d", result.EstimatedInputTokens)
	}
	for _, message := range result.Messages {
		if message.Role == domain.ContextRoleTool {
			t.Fatalf("historical tool result must not be emitted as an orphan tool protocol message: %+v", message)
		}
	}
	if !strings.Contains(joined, strings.Repeat("继续聊很长的内容", 2000)) {
		t.Fatal("current input was cropped based on token estimate")
	}
	assertTrustedContextQuery(t, messages.userID, messages.conversationID)
	if tools.userID != 42 || tools.conversationID != "conv-1" {
		t.Fatalf("tool query user=%d conversation=%q", tools.userID, tools.conversationID)
	}
}

func TestManagerFailsWithoutRequiredMessageSourceAndRejectsInvalidRequests(t *testing.T) {
	manager := NewManager(nil, nil)
	for _, req := range []domain.BuildContextRequest{
		{ConversationID: "conv-1", Mode: domain.AgentContextMode, CurrentInput: "hello"},
		{UserID: 42, Mode: domain.AgentContextMode, CurrentInput: "hello"},
		{UserID: 42, ConversationID: "conv-1", Mode: "unknown", CurrentInput: "hello"},
		{UserID: 42, ConversationID: "conv-1", Mode: domain.AgentContextMode},
	} {
		if _, err := manager.Build(context.Background(), req); err == nil {
			t.Fatalf("Build(%+v) error = nil", req)
		}
	}

	messageErr := errors.New("messages unavailable")
	manager = NewManager(&fakeContextMessageStore{err: messageErr}, nil)
	_, err := manager.Build(context.Background(), domain.BuildContextRequest{
		UserID: 42, ConversationID: "conv-1", Mode: domain.AgentContextMode, CurrentInput: "hello",
	})
	if !errors.Is(err, messageErr) {
		t.Fatalf("Build() error = %v, want %v", err, messageErr)
	}
}

func TestManagerRedactsSensitiveValuesFromRecentMessagesWithoutCropping(t *testing.T) {
	longHistory := strings.Repeat("商品 12 的完整说明", 100)
	manager := NewManager(&fakeContextMessageStore{messages: []*aimessages.AiMessages{
		contextMessage("m1", "user", "token=secret-token user_id:999 auth:bearer "+longHistory, baseTime()),
	}}, nil)

	result, err := manager.Build(context.Background(), domain.BuildContextRequest{
		UserID: 42, ConversationID: "conv-1", Mode: domain.AgentContextMode, CurrentInput: "继续",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := joinContextContents(result.Messages)
	for _, leaked := range []string{"secret-token", "user_id:999", "auth:bearer"} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("context leaked %q: %s", leaked, joined)
		}
	}
	if !strings.Contains(joined, longHistory) {
		t.Fatal("recent message was cropped")
	}
}

func TestManagerAgentContextExcludesMessagesCoveredBySummaryWatermark(t *testing.T) {
	watermarkTime := baseTime().Add(time.Minute)
	manager := NewManager(&fakeContextMessageStore{messages: []*aimessages.AiMessages{
		contextMessage("m-before", "user", "already summarized before", baseTime()),
		contextMessage("m-covered", "assistant", "already summarized at watermark", watermarkTime),
		contextMessage("m-after", "user", "unsummarized recent", watermarkTime.Add(time.Minute)),
	}}, nil, WithSummaryStore(&fakeSummaryStore{summary: &domain.ConversationSummary{
		Summary: "summary content", CoveredUntilMessageID: "m-covered", CoveredUntilCreatedAt: watermarkTime,
	}}))

	result, err := manager.Build(context.Background(), domain.BuildContextRequest{
		UserID: 42, ConversationID: "conv-1", Mode: domain.AgentContextMode, CurrentInput: "继续",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := joinContextContents(result.Messages)
	if strings.Contains(joined, "already summarized") || !strings.Contains(joined, "unsummarized recent") {
		t.Fatalf("AgentContext summary overlap: %s", joined)
	}
	if result.RecentMessageStartID != "m-after" || result.RecentMessageEndID != "m-after" {
		t.Fatalf("recent range = %q..%q", result.RecentMessageStartID, result.RecentMessageEndID)
	}
}

func TestManagerSkipsUserProfileWhenStoreFails(t *testing.T) {
	manager := NewManager(&fakeContextMessageStore{messages: []*aimessages.AiMessages{
		contextMessage("m1", "user", "想买手机", baseTime()),
	}}, nil, WithUserProfileStore(&fakeUserProfileStore{err: errors.New("profile unavailable")}))

	result, err := manager.Build(context.Background(), domain.BuildContextRequest{
		UserID: 42, ConversationID: "conv-1", Mode: domain.AgentContextMode, CurrentInput: "继续",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := joinContextContents(result.Messages)
	if strings.Contains(joined, "[user_profile]") {
		t.Fatalf("profile store error should degrade by skipping profile: %s", joined)
	}
}

type fakeContextMessageStore struct {
	messages       []*aimessages.AiMessages
	err            error
	userID         uint64
	conversationID string
	limit          int
}

func (f *fakeContextMessageStore) FindRecent(_ context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error) {
	f.userID = userID
	f.conversationID = conversationID
	f.limit = limit
	return f.messages, f.err
}

type fakeSummaryStore struct {
	summary *domain.ConversationSummary
	err     error
	calls   int
}

func (f *fakeSummaryStore) FindLatest(context.Context, uint64, string) (*domain.ConversationSummary, error) {
	f.calls++
	return f.summary, f.err
}

type fakeTaskStateStore struct {
	state *domain.TaskState
	err   error
}

func (f *fakeTaskStateStore) FindActive(context.Context, uint64, string, string) (*domain.TaskState, error) {
	return f.state, f.err
}

type fakeUserProfileStore struct {
	profile *domain.UserProfile
	err     error
	calls   int
	userID  uint64
}

func (f *fakeUserProfileStore) LoadActive(_ context.Context, userID uint64) (*domain.UserProfile, error) {
	f.calls++
	f.userID = userID
	return f.profile, f.err
}

type fakeToolContextStore struct {
	latest         *aitoolcalls.AiToolCalls
	recent         []*aitoolcalls.AiToolCalls
	err            error
	latestCalls    int
	recentCalls    int
	userID         uint64
	conversationID string
}

func (f *fakeToolContextStore) FindLatestToolResult(_ context.Context, userID uint64, conversationID string) (*aitoolcalls.AiToolCalls, error) {
	f.latestCalls++
	f.userID = userID
	f.conversationID = conversationID
	return f.latest, f.err
}

func (f *fakeToolContextStore) FindRecentToolCall(_ context.Context, userID uint64, conversationID string, _ int) ([]*aitoolcalls.AiToolCalls, error) {
	f.recentCalls++
	f.userID = userID
	f.conversationID = conversationID
	return f.recent, f.err
}

func contextMessage(id, role, content string, createdAt time.Time) *aimessages.AiMessages {
	return &aimessages.AiMessages{
		MsgId: id, UserId: 42, ConversationId: "conv-1", Role: role, Content: content, CreatedAt: createdAt,
	}
}

func contextMessageID(index int) string {
	if index < 10 {
		return "m" + string(rune('0'+index))
	}
	return "m" + string(rune('0'+index/10)) + string(rune('0'+index%10))
}

func joinContextContents(messages []domain.ContextMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func countContextContent(messages []domain.ContextMessage, content string) int {
	count := 0
	for _, message := range messages {
		if message.Content == content {
			count++
		}
	}
	return count
}

func assertTrustedContextQuery(t *testing.T, userID uint64, conversationID string) {
	t.Helper()
	if userID != 42 || conversationID != "conv-1" {
		t.Fatalf("context query user=%d conversation=%q", userID, conversationID)
	}
}
