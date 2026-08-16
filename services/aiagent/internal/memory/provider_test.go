package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestCustomerServiceProviderRetrieveSplitsHistoryAndRuntimeContext(t *testing.T) {
	base := time.Date(2026, 8, 14, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	provider := NewCustomerServiceProvider(CustomerServiceProviderConfig{
		Messages: &fakeMessageStore{rows: []*aimessages.AiMessages{
			messageRow("m-summarized", domain.ContextRoleUser, "已经被摘要覆盖", base),
			messageRow("m-current", domain.ContextRoleUser, "当前输入", base.Add(time.Minute)),
			messageRow("m-recent", domain.ContextRoleAssistant, "近期回复 token=secret", base.Add(2*time.Minute)),
		}},
		Summaries: &fakeSummaryStore{summary: &domain.ConversationSummary{
			Summary:               "更早摘要",
			CoveredUntilMessageID: "m-summarized",
			CoveredUntilCreatedAt: base,
		}},
		Tools: &fakeToolStore{
			latest: &aitoolcalls.AiToolCalls{
				ToolCallId: "call-latest",
				ToolName:   domain.ToolCartList,
				Status:     "success",
				Result:     `{"items":[{"cart_item_id":7}]}`,
			},
			recent: []*aitoolcalls.AiToolCalls{
				{ToolCallId: "call-latest", ToolName: domain.ToolCartList, Status: "success", Result: `{"items":[{"cart_item_id":7}]}`},
				{ToolCallId: "call-old", ToolName: domain.ToolProductRecommend, Status: "success", Result: `{"products":[{"product_id":12}]}`, CreatedAt: base},
			},
		},
		Profiles: &fakeProfileStore{profile: &domain.UserProfile{ProfileJSON: json.RawMessage(`{"preferences":{"categories":["手机"]}}`)}},
		Now:      func() time.Time { return base },
	})

	result, err := provider.Retrieve(context.Background(), &RetrieveRequest{
		UserID:           42,
		ConversationID:   "conv-1",
		CurrentMessageID: "m-current",
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(result.SystemMessages) != 0 {
		t.Fatalf("SystemMessages len = %d, want 0 from provider", len(result.SystemMessages))
	}
	if len(result.HistoryMessages) != 1 || result.HistoryMessages[0].Content != "近期回复 token=[redacted]" {
		t.Fatalf("HistoryMessages = %+v, want only unsummarized redacted recent assistant", result.HistoryMessages)
	}
	joined := joinMessages(result.ContextMessages)
	for _, want := range []string{
		"<current_time>",
		"<conversation_context>",
		"更早摘要",
		"<latest_tool_result>",
		"cart_item_id",
		"<recent_tool_calls>",
		"call-old",
		"<user_profile>",
		`"categories":["手机"]`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("runtime context missing %q: %s", want, joined)
		}
	}
	for _, forbidden := range []string{"已经被摘要覆盖", "当前输入", "call-latest\""} {
		if strings.Contains(joinMessages(result.HistoryMessages), forbidden) {
			t.Fatalf("history should not contain %q: %+v", forbidden, result.HistoryMessages)
		}
	}
	if strings.Contains(joined, "预算 3000 元") {
		t.Fatalf("runtime context should not include removed atomic memories: %s", joined)
	}
	if strings.Contains(joined, `"products":[{"product_id":12}]`) {
		t.Fatalf("recent_tool_calls should not include historical result payload: %s", joined)
	}
	if got := result.Metadata["latest_tool_call_id"]; got != "call-latest" {
		t.Fatalf("latest tool metadata = %v, want call-latest", got)
	}
	if got := result.Metadata["recent_tool_call_count"]; got != 1 {
		t.Fatalf("recent tool call count = %v, want 1", got)
	}
}

func TestCustomerServiceProviderMemorizeRunsHooksWithFinalMessages(t *testing.T) {
	summary := &fakeSummaryRefresher{}
	profile := &fakeProfilePublisher{}
	provider := NewCustomerServiceProvider(CustomerServiceProviderConfig{
		SummaryRefresher: summary,
		ProfilePublisher: profile,
	})

	err := provider.Memorize(context.Background(), &MemorizeRequest{
		UserID:         42,
		ConversationID: "conv-1",
		Messages: []domain.ContextMessage{
			{Role: domain.ContextRoleUser, Content: "原始用户输入"},
			{Role: domain.ContextRoleAssistant, Content: "最终回答"},
		},
		MessageIDs: []string{"msg-user", "msg-final"},
	})
	if err != nil {
		t.Fatalf("Memorize() error = %v", err)
	}
	if summary.userID != 42 || summary.conversationID != "conv-1" {
		t.Fatalf("summary refresh user=%d conversation=%q", summary.userID, summary.conversationID)
	}
	if profile.userID != 42 || profile.conversationID != "conv-1" || len(profile.messageIDs) != 2 {
		t.Fatalf("profile update = user:%d conversation:%q ids:%+v", profile.userID, profile.conversationID, profile.messageIDs)
	}
}

func TestConversationValuesUseConversationIDOnly(t *testing.T) {
	values := NewConversationValues(ConversationMetadata{
		UserID:           42,
		ConversationID:   "conv-1",
		RunID:            "run-1",
		CurrentMessageID: "msg-user",
		ClientMessageID:  "client-1",
	})
	if _, ok := values["session"+"ID"]; ok {
		t.Fatalf("conversation values contain unexpected legacy key: %+v", values)
	}
	meta, ok := ConversationMetadataFromMap(values)
	if !ok {
		t.Fatal("ConversationMetadataFromMap() ok = false")
	}
	if meta.ConversationID != "conv-1" || meta.UserID != 42 {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestConversationMetadataRequiresConversationID(t *testing.T) {
	values := map[string]any{
		ConversationValueKeyUserID: 42,
	}
	if meta, ok := ConversationMetadataFromMap(values); ok {
		t.Fatalf("ConversationMetadataFromMap() = %+v, true; want false without conversationID", meta)
	}
}

type fakeMessageStore struct {
	rows []*aimessages.AiMessages
	err  error
}

func (f *fakeMessageStore) FindRecent(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error) {
	return f.rows, f.err
}

type fakeSummaryStore struct {
	summary *domain.ConversationSummary
	err     error
}

func (f *fakeSummaryStore) FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error) {
	return f.summary, f.err
}

type fakeToolStore struct {
	latest *aitoolcalls.AiToolCalls
	recent []*aitoolcalls.AiToolCalls
	err    error
}

func (f *fakeToolStore) FindLatestToolResult(ctx context.Context, userID uint64, conversationID string) (*aitoolcalls.AiToolCalls, error) {
	return f.latest, f.err
}

func (f *fakeToolStore) FindRecentToolCall(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aitoolcalls.AiToolCalls, error) {
	return f.recent, f.err
}

type fakeProfileStore struct {
	profile *domain.UserProfile
	err     error
}

func (f *fakeProfileStore) LoadActive(ctx context.Context, userID uint64) (*domain.UserProfile, error) {
	return f.profile, f.err
}

type fakeSummaryRefresher struct {
	userID         uint64
	conversationID string
}

func (f *fakeSummaryRefresher) RefreshMemorySummary(ctx context.Context, userID uint64, conversationID string) error {
	f.userID = userID
	f.conversationID = conversationID
	return nil
}

type fakeProfilePublisher struct {
	userID         uint64
	conversationID string
	messageIDs     []string
}

func (f *fakeProfilePublisher) PublishMemoryUpdate(ctx context.Context, userID uint64, conversationID string, messageIDs []string) error {
	f.userID = userID
	f.conversationID = conversationID
	f.messageIDs = messageIDs
	return nil
}

func messageRow(id, role, content string, createdAt time.Time) *aimessages.AiMessages {
	return &aimessages.AiMessages{MsgId: id, Role: role, Content: content, CreatedAt: createdAt, UserId: 42, ConversationId: "conv-1"}
}

func joinMessages(messages []domain.ContextMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}
