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
		UserID:          42,
		ClientMessageID: "client-1",
		Content:         "你好",
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
	if messages.inserted.MsgId != prepared.UserMessageID {
		t.Fatalf("inserted message msg_id = %q, want %q", messages.inserted.MsgId, prepared.UserMessageID)
	}
	if messages.inserted.ClientMessageId.String != "client-1" || !messages.inserted.ClientMessageId.Valid {
		t.Fatalf("inserted message client_message_id = %+v", messages.inserted.ClientMessageId)
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
}

func TestPrepareReturnsDuplicateWhenClientMessageIDAlreadyExists(t *testing.T) {
	ctx := context.Background()
	conversations := newFakeConversationsModel()
	messages := newFakeMessagesModel()
	messages.duplicate = &aimessages.AiMessages{
		Id:              9,
		MsgId:           "msg_existing",
		ConversationId:  "conv_existing",
		UserId:          42,
		Role:            RoleUser,
		ClientMessageId: sql.NullString{String: "client-1", Valid: true},
		Content:         "你好",
	}
	manager := NewManager(conversations, messages)

	prepared, err := manager.Prepare(ctx, PrepareRequest{
		UserID:          42,
		ConversationID:  "conv_existing",
		ClientMessageID: "client-1",
		Content:         "你好",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if !prepared.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}
	if prepared.ConversationID != "conv_existing" || prepared.UserMessageID != "msg_existing" {
		t.Fatalf("prepared duplicate = %+v", prepared)
	}
	if messages.inserted != nil {
		t.Fatalf("duplicate request should not insert message: %+v", messages.inserted)
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
		UserID:          7,
		ConversationID:  "conv_existing",
		ClientMessageID: "client-2",
		Content:         "继续聊",
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
		UserID:          7,
		ConversationID:  "conv_other",
		ClientMessageID: "client-3",
		Content:         "越权访问",
	})
	if !errors.Is(err, ErrConversationForbidden) {
		t.Fatalf("Prepare error = %v, want ErrConversationForbidden", err)
	}
	if messages.inserted != nil {
		t.Fatal("message should not be inserted for forbidden conversation")
	}
}

func TestPrepareDoesNotLoadHistoryBecauseContextManagerOwnsContext(t *testing.T) {
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
			MsgId:          "old_msg",
			ConversationId: "conv_history",
			UserId:         8,
			Role:           RoleAssistant,
			Content:        "old",
			CreatedAt:      time.Unix(int64(i), 0),
		})
	}
	manager := NewManager(conversations, messages, WithHistoryLimit(3))

	prepared, err := manager.Prepare(ctx, PrepareRequest{
		UserID:          8,
		ConversationID:  "conv_history",
		ClientMessageID: "client-4",
		Content:         "最新消息",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	if prepared.ConversationID != "conv_history" || prepared.UserMessageID == "" {
		t.Fatalf("prepared = %+v", prepared)
	}
	if messages.lastLimit != 0 || messages.lastConversation != "" {
		t.Fatalf("conversation manager should not load history, lastConversation=%q lastLimit=%d", messages.lastConversation, messages.lastLimit)
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
	duplicate        *aimessages.AiMessages
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

func (m *fakeMessagesModel) FindUserMessageByClientMessageID(_ context.Context, userID uint64, clientMessageID string) (*aimessages.AiMessages, error) {
	if m.duplicate == nil || m.duplicate.UserId != userID || !m.duplicate.ClientMessageId.Valid || m.duplicate.ClientMessageId.String != clientMessageID {
		return nil, aimessages.ErrNotFound
	}
	copied := *m.duplicate
	return &copied, nil
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
