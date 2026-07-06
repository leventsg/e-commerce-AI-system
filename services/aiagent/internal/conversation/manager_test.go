package conversation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
)

func TestPrepareCreatesConversationAndStoresUserMessage(t *testing.T) {
	ctx := context.Background()
	conversations := newFakeConversationsModel()
	messages := newFakeMessagesModel()
	manager := NewManager(conversations, messages)

	prepared, err := manager.Prepare(ctx, PrepareRequest{
		UserID:  42,
		Content: "你好",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	if prepared.ConversationID == "" || prepared.ConversationID[:5] != "conv_" {
		t.Fatalf("ConversationID = %q, want conv_ prefix", prepared.ConversationID)
	}
	if prepared.UserMessageID == "" || prepared.UserMessageID[:4] != "msg_" {
		t.Fatalf("UserMessageID = %q, want msg_ prefix", prepared.UserMessageID)
	}
	if len(prepared.History) != 1 {
		t.Fatalf("len(History) = %d, want 1", len(prepared.History))
	}
	if conversations.inserted == nil {
		t.Fatal("conversation was not inserted")
	}
	if conversations.inserted.Id != prepared.ConversationID {
		t.Fatalf("inserted conversation id = %q, want %q", conversations.inserted.Id, prepared.ConversationID)
	}
	if conversations.inserted.UserId != 42 {
		t.Fatalf("inserted conversation user_id = %d, want 42", conversations.inserted.UserId)
	}
	if conversations.inserted.Status != StatusActive {
		t.Fatalf("inserted conversation status = %q, want %q", conversations.inserted.Status, StatusActive)
	}
	if messages.inserted == nil {
		t.Fatal("user message was not inserted")
	}
	if messages.inserted.ConversationId != prepared.ConversationID {
		t.Fatalf("inserted message conversation_id = %q, want %q", messages.inserted.ConversationId, prepared.ConversationID)
	}
	if messages.inserted.UserId != 42 {
		t.Fatalf("inserted message user_id = %d, want 42", messages.inserted.UserId)
	}
	if messages.inserted.Role != RoleUser {
		t.Fatalf("inserted message role = %q, want %q", messages.inserted.Role, RoleUser)
	}
	if messages.inserted.Content != "你好" {
		t.Fatalf("inserted message content = %q, want 你好", messages.inserted.Content)
	}
	if prepared.History[0].Id != prepared.UserMessageID {
		t.Fatalf("history message id = %q, want %q", prepared.History[0].Id, prepared.UserMessageID)
	}
}

func TestPrepareRestoresConversationForSameUser(t *testing.T) {
	ctx := context.Background()
	conversations := newFakeConversationsModel()
	messages := newFakeMessagesModel()
	conversations.rows["conv_existing"] = &aiconversations.AiConversations{
		Id:     "conv_existing",
		UserId: 7,
		Status: StatusActive,
	}
	manager := NewManager(conversations, messages)

	prepared, err := manager.Prepare(ctx, PrepareRequest{
		UserID:         7,
		ConversationID: "conv_existing",
		Content:        "继续聊",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	if prepared.ConversationID != "conv_existing" {
		t.Fatalf("ConversationID = %q, want conv_existing", prepared.ConversationID)
	}
	if conversations.inserted != nil {
		t.Fatal("existing conversation should not be inserted again")
	}
	if messages.inserted == nil {
		t.Fatal("user message was not inserted")
	}
	if messages.inserted.ConversationId != "conv_existing" {
		t.Fatalf("inserted message conversation_id = %q, want conv_existing", messages.inserted.ConversationId)
	}
}

func TestPrepareRejectsConversationOwnedByAnotherUser(t *testing.T) {
	ctx := context.Background()
	conversations := newFakeConversationsModel()
	messages := newFakeMessagesModel()
	conversations.rows["conv_other"] = &aiconversations.AiConversations{
		Id:     "conv_other",
		UserId: 99,
		Status: StatusActive,
	}
	manager := NewManager(conversations, messages)

	_, err := manager.Prepare(ctx, PrepareRequest{
		UserID:         7,
		ConversationID: "conv_other",
		Content:        "越权访问",
	})
	if !errors.Is(err, ErrConversationForbidden) {
		t.Fatalf("Prepare error = %v, want ErrConversationForbidden", err)
	}
	if messages.inserted != nil {
		t.Fatal("message should not be inserted for forbidden conversation")
	}
}

func TestPrepareUsesRecentHistoryLimitInChronologicalOrder(t *testing.T) {
	ctx := context.Background()
	conversations := newFakeConversationsModel()
	messages := newFakeMessagesModel()
	conversations.rows["conv_history"] = &aiconversations.AiConversations{
		Id:     "conv_history",
		UserId: 8,
		Status: StatusActive,
	}
	for i := 0; i < 25; i++ {
		messages.rows = append(messages.rows, &aimessages.AiMessages{
			Id:             "old_msg",
			ConversationId: "conv_history",
			UserId:         8,
			Role:           RoleAssistant,
			Content:        "old",
			CreatedAt:      time.Unix(int64(i), 0),
		})
	}
	manager := NewManager(conversations, messages, WithHistoryLimit(3))

	prepared, err := manager.Prepare(ctx, PrepareRequest{
		UserID:         8,
		ConversationID: "conv_history",
		Content:        "最新消息",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	if messages.lastLimit != 3 {
		t.Fatalf("FindRecentByConversationID limit = %d, want 3", messages.lastLimit)
	}
	if len(prepared.History) != 3 {
		t.Fatalf("len(History) = %d, want 3", len(prepared.History))
	}
	if prepared.History[0].Content != "old" || prepared.History[1].Content != "old" || prepared.History[2].Content != "最新消息" {
		t.Fatalf("history contents = [%q, %q, %q], want old, old, 最新消息",
			prepared.History[0].Content,
			prepared.History[1].Content,
			prepared.History[2].Content)
	}
	if prepared.History[2].Role != RoleUser {
		t.Fatalf("last history role = %q, want %q", prepared.History[2].Role, RoleUser)
	}
}

type fakeConversationsModel struct {
	rows     map[string]*aiconversations.AiConversations
	inserted *aiconversations.AiConversations
}

func newFakeConversationsModel() *fakeConversationsModel {
	return &fakeConversationsModel{rows: make(map[string]*aiconversations.AiConversations)}
}

func (m *fakeConversationsModel) Insert(_ context.Context, data *aiconversations.AiConversations) (sql.Result, error) {
	copied := *data
	m.inserted = &copied
	m.rows[data.Id] = &copied
	return nil, nil
}

func (m *fakeConversationsModel) FindOne(_ context.Context, id string) (*aiconversations.AiConversations, error) {
	row, ok := m.rows[id]
	if !ok {
		return nil, aiconversations.ErrNotFound
	}
	copied := *row
	return &copied, nil
}

type fakeMessagesModel struct {
	rows             []*aimessages.AiMessages
	inserted         *aimessages.AiMessages
	lastConversation string
	lastLimit        int
}

func newFakeMessagesModel() *fakeMessagesModel {
	return &fakeMessagesModel{}
}

func (m *fakeMessagesModel) Insert(_ context.Context, data *aimessages.AiMessages) (sql.Result, error) {
	copied := *data
	m.inserted = &copied
	m.rows = append(m.rows, &copied)
	return nil, nil
}

func (m *fakeMessagesModel) FindRecentByConversationID(_ context.Context, conversationID string, limit int) ([]*aimessages.AiMessages, error) {
	m.lastConversation = conversationID
	m.lastLimit = limit
	var filtered []*aimessages.AiMessages
	for _, row := range m.rows {
		if row.ConversationId == conversationID {
			filtered = append(filtered, row)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := make([]*aimessages.AiMessages, 0, len(filtered))
	for _, row := range filtered {
		copied := *row
		out = append(out, &copied)
	}
	return out, nil
}
