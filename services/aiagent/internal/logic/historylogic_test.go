package logic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
)

type historyFakeConversationsModel struct {
	aiconversations.AiConversationsModel
	conversation *aiconversations.AiConversations
	items        []*aiconversations.ConversationListItem
	total        int64
	findErr      error
	countErr     error
	queryErr     error
	lastLimit    int
	lastOffset   int
}

func (m *historyFakeConversationsModel) FindOne(_ context.Context, _ string) (*aiconversations.AiConversations, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.conversation == nil {
		return nil, aiconversations.ErrNotFound
	}
	return m.conversation, nil
}

func (m *historyFakeConversationsModel) CountByUser(_ context.Context, _ uint64) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.total, nil
}

func (m *historyFakeConversationsModel) FindByUserWithStats(_ context.Context, _ uint64, limit, offset int) ([]*aiconversations.ConversationListItem, error) {
	m.lastLimit, m.lastOffset = limit, offset
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.items, nil
}

type historyFakeMessagesModel struct {
	aimessages.AiMessagesModel
	rows       []*aimessages.AiMessages
	total      int64
	countErr   error
	queryErr   error
	lastLimit  int
	lastOffset int
}

func (m *historyFakeMessagesModel) CountByUserAndConversation(_ context.Context, _ uint64, _ string) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.total, nil
}

func (m *historyFakeMessagesModel) FindByUserAndConversation(_ context.Context, _ uint64, _ string, limit, offset int) ([]*aimessages.AiMessages, error) {
	m.lastLimit, m.lastOffset = limit, offset
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.rows, nil
}

func TestListConversationsReturnsUserSessions(t *testing.T) {
	lastMessageAt := time.Date(2026, 8, 20, 15, 6, 37, 0, time.FixedZone("CST", 8*3600))
	conversations := &historyFakeConversationsModel{
		total: 2,
		items: []*aiconversations.ConversationListItem{
			{
				Id:                 "conv-1",
				UserId:             42,
				Title:              "订单咨询",
				Status:             "active",
				CreatedAt:          time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
				UpdatedAt:          time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
				LastMessageAt:      sql.NullTime{Time: lastMessageAt, Valid: true},
				LastMessagePreview: "你的订单已送达",
				MessageCount:       6,
			},
			{
				Id:        "conv-2",
				UserId:    42,
				Title:     "新会话",
				Status:    "active",
				CreatedAt: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
			},
		},
	}
	logic := NewListConversationsLogic(context.Background(), &svc.ServiceContext{ConversationsModel: conversations})

	resp, err := logic.ListConversations(&aiagent.ListConversationsRequest{UserId: 42, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if conversations.lastLimit != 10 || conversations.lastOffset != 0 {
		t.Fatalf("query limit=%d offset=%d, want 10,0", conversations.lastLimit, conversations.lastOffset)
	}
	if resp.Total != 2 || len(resp.Conversations) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	first := resp.Conversations[0]
	if first.ConversationId != "conv-1" || first.Title != "订单咨询" || first.LastMessagePreview != "你的订单已送达" || first.MessageCount != 6 {
		t.Fatalf("first conversation = %+v", first)
	}
	if first.UpdatedAt != "2026-08-20T15:06:37+08:00" {
		t.Fatalf("updated_at = %q, want last message time", first.UpdatedAt)
	}
	second := resp.Conversations[1]
	if second.UpdatedAt != "2026-08-20T11:00:00Z" || second.MessageCount != 0 {
		t.Fatalf("second conversation updated_at=%q message_count=%d, want created_at fallback and 0", second.UpdatedAt, second.MessageCount)
	}
}

func TestListConversationsRejectsInvalidUser(t *testing.T) {
	logic := NewListConversationsLogic(context.Background(), &svc.ServiceContext{})
	if _, err := logic.ListConversations(&aiagent.ListConversationsRequest{}); err == nil {
		t.Fatal("ListConversations without user_id should fail")
	}
}

func TestListConversationsSurfacesCountAndQueryErrors(t *testing.T) {
	countErr := NewListConversationsLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: &historyFakeConversationsModel{countErr: errors.New("db down")},
	})
	if _, err := countErr.ListConversations(&aiagent.ListConversationsRequest{UserId: 42}); err == nil || !strings.Contains(err.Error(), "历史会话加载失败") {
		t.Fatalf("count error = %v", err)
	}

	queryErr := NewListConversationsLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: &historyFakeConversationsModel{queryErr: errors.New("db down")},
	})
	if _, err := queryErr.ListConversations(&aiagent.ListConversationsRequest{UserId: 42}); err == nil || !strings.Contains(err.Error(), "历史会话加载失败") {
		t.Fatalf("query error = %v", err)
	}
}

func TestListConversationsNormalizesPagination(t *testing.T) {
	conversations := &historyFakeConversationsModel{}
	logic := NewListConversationsLogic(context.Background(), &svc.ServiceContext{ConversationsModel: conversations})

	if _, err := logic.ListConversations(&aiagent.ListConversationsRequest{UserId: 42}); err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if conversations.lastLimit != 50 || conversations.lastOffset != 0 {
		t.Fatalf("default limit=%d offset=%d, want 50,0", conversations.lastLimit, conversations.lastOffset)
	}

	if _, err := logic.ListConversations(&aiagent.ListConversationsRequest{UserId: 42, Page: 2, PageSize: 1000}); err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if conversations.lastLimit != 200 || conversations.lastOffset != 200 {
		t.Fatalf("capped limit=%d offset=%d, want 200,200", conversations.lastLimit, conversations.lastOffset)
	}
}

func TestListMessagesReturnsAllRoles(t *testing.T) {
	createdAt := time.Date(2026, 8, 20, 15, 0, 0, 0, time.FixedZone("CST", 8*3600))
	conversations := &historyFakeConversationsModel{conversation: &aiconversations.AiConversations{Id: "conv-1", UserId: 42}}
	messages := &historyFakeMessagesModel{
		total: 3,
		rows: []*aimessages.AiMessages{
			{MsgId: "msg-1", ConversationId: "conv-1", UserId: 42, Role: "user", Content: "查一下订单", CreatedAt: createdAt},
			{MsgId: "msg-2", ConversationId: "conv-1", UserId: 42, Role: "assistant", Content: "好的", CreatedAt: createdAt.Add(time.Second)},
			{
				MsgId:           "msg-3",
				ConversationId:  "conv-1",
				UserId:          42,
				Role:            "tool",
				Content:         "查询结果",
				Metadata:        sql.NullString{String: `{"tool_name":"order_get","status":"success"}`, Valid: true},
				ClientMessageId: sql.NullString{String: "client-1", Valid: true},
				CreatedAt:       createdAt.Add(2 * time.Second),
			},
		},
	}
	logic := NewListMessagesLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: conversations,
		MessagesModel:      messages,
	})

	resp, err := logic.ListMessages(&aiagent.ListMessagesRequest{UserId: 42, ConversationId: "conv-1", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if messages.lastLimit != 50 || messages.lastOffset != 0 {
		t.Fatalf("query limit=%d offset=%d, want 50,0", messages.lastLimit, messages.lastOffset)
	}
	if resp.ConversationId != "conv-1" || resp.Total != 3 || len(resp.Messages) != 3 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Messages[0].Role != "user" || resp.Messages[1].Role != "assistant" || resp.Messages[2].Role != "tool" {
		t.Fatalf("roles = %q, %q, %q", resp.Messages[0].Role, resp.Messages[1].Role, resp.Messages[2].Role)
	}
	tool := resp.Messages[2]
	if tool.MessageId != "msg-3" || tool.ClientMessageId != "client-1" || tool.MetadataJson != `{"tool_name":"order_get","status":"success"}` {
		t.Fatalf("tool message = %+v", tool)
	}
	if tool.CreatedAt != "2026-08-20T15:00:02+08:00" {
		t.Fatalf("tool created_at = %q", tool.CreatedAt)
	}
}

func TestListMessagesRejectsInvalidInput(t *testing.T) {
	conversations := &historyFakeConversationsModel{conversation: &aiconversations.AiConversations{Id: "conv-1", UserId: 42}}
	logic := NewListMessagesLogic(context.Background(), &svc.ServiceContext{ConversationsModel: conversations})

	if _, err := logic.ListMessages(&aiagent.ListMessagesRequest{}); err == nil {
		t.Fatal("missing user should fail")
	}
	if _, err := logic.ListMessages(&aiagent.ListMessagesRequest{UserId: 42, ConversationId: "  "}); err == nil {
		t.Fatal("missing conversation_id should fail")
	}
}

func TestListMessagesRejectsMissingConversation(t *testing.T) {
	logic := NewListMessagesLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: &historyFakeConversationsModel{},
	})
	_, err := logic.ListMessages(&aiagent.ListMessagesRequest{UserId: 42, ConversationId: "conv-missing"})
	if err == nil || err.Error() != "会话不存在" {
		t.Fatalf("error = %v, want 会话不存在", err)
	}
}

func TestListMessagesRejectsCrossUserConversation(t *testing.T) {
	logic := NewListMessagesLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: &historyFakeConversationsModel{
			conversation: &aiconversations.AiConversations{Id: "conv-1", UserId: 99},
		},
	})
	_, err := logic.ListMessages(&aiagent.ListMessagesRequest{UserId: 42, ConversationId: "conv-1"})
	if err == nil || err.Error() != "无权访问该会话" {
		t.Fatalf("error = %v, want 无权访问该会话", err)
	}
}

func TestListMessagesSurfacesCountAndQueryErrors(t *testing.T) {
	conversations := &historyFakeConversationsModel{conversation: &aiconversations.AiConversations{Id: "conv-1", UserId: 42}}

	countErr := NewListMessagesLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: conversations,
		MessagesModel:      &historyFakeMessagesModel{countErr: errors.New("db down")},
	})
	if _, err := countErr.ListMessages(&aiagent.ListMessagesRequest{UserId: 42, ConversationId: "conv-1"}); err == nil || !strings.Contains(err.Error(), "历史消息加载失败") {
		t.Fatalf("count error = %v", err)
	}

	queryErr := NewListMessagesLogic(context.Background(), &svc.ServiceContext{
		ConversationsModel: conversations,
		MessagesModel:      &historyFakeMessagesModel{queryErr: errors.New("db down")},
	})
	if _, err := queryErr.ListMessages(&aiagent.ListMessagesRequest{UserId: 42, ConversationId: "conv-1"}); err == nil || !strings.Contains(err.Error(), "历史消息加载失败") {
		t.Fatalf("query error = %v", err)
	}
}
