package logic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
)

func TestListSessionsRequiresAuthenticatedUser(t *testing.T) {
	l := NewHistoryLogic(context.Background(), &svc.ServiceContext{})
	if _, err := l.ListSessions(&types.ListSessionsRequest{}); err == nil {
		t.Fatal("ListSessions without user_id should fail")
	}
}

func TestListSessionsForwardsUserAndMapsResponse(t *testing.T) {
	rpc := &fakeAiAgent{listConversationsResp: &aiagent.ListConversationsResponse{
		Total: 1,
		Conversations: []*aiagent.ConversationSummary{{
			ConversationId:     "conv-1",
			Title:              "订单咨询",
			LastMessagePreview: "你的订单已送达",
			UpdatedAt:          "2026-08-20T15:06:37+08:00",
			MessageCount:       3,
		}},
	}}
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(42))
	l := NewHistoryLogic(ctx, &svc.ServiceContext{AiAgentRpc: rpc})

	resp, err := l.ListSessions(&types.ListSessionsRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if rpc.listConversationsReq == nil || rpc.listConversationsReq.UserId != 42 || rpc.listConversationsReq.Page != 1 || rpc.listConversationsReq.PageSize != 10 {
		t.Fatalf("rpc request = %+v", rpc.listConversationsReq)
	}
	if resp.Total != 1 || len(resp.Conversations) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	item := resp.Conversations[0]
	if item.ConversationID != "conv-1" || item.Title != "订单咨询" || item.LastMessagePreview != "你的订单已送达" || item.UpdatedAt != "2026-08-20T15:06:37+08:00" || item.MessageCount != 3 {
		t.Fatalf("session summary = %+v", item)
	}
}

func TestListSessionsMapsRPCErrorToServerError(t *testing.T) {
	rpc := &fakeAiAgent{listConversationsErr: errors.New("rpc down")}
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(42))
	l := NewHistoryLogic(ctx, &svc.ServiceContext{AiAgentRpc: rpc})

	_, err := l.ListSessions(&types.ListSessionsRequest{})
	if err == nil || !strings.Contains(err.Error(), code.ServerErrorMsg) {
		t.Fatalf("ListSessions error = %v, want server error", err)
	}
}

func TestListMessagesRequiresConversationID(t *testing.T) {
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(42))
	l := NewHistoryLogic(ctx, &svc.ServiceContext{AiAgentRpc: &fakeAiAgent{}})

	if _, err := l.ListMessages(&types.ListMessagesRequest{ConversationID: "  "}); err == nil {
		t.Fatal("ListMessages without conversation_id should fail")
	}
}

func TestListMessagesForwardsUserAndMapsAllRoles(t *testing.T) {
	rpc := &fakeAiAgent{listMessagesResp: &aiagent.ListMessagesResponse{
		ConversationId: "conv-1",
		Total:          3,
		Messages: []*aiagent.HistoryMessage{
			{MessageId: "msg-1", Role: "user", Content: "查一下订单", CreatedAt: "2026-08-20T15:00:00+08:00"},
			{MessageId: "msg-2", Role: "assistant", Content: "好的", CreatedAt: "2026-08-20T15:00:01+08:00"},
			{MessageId: "msg-3", Role: "tool", Content: "查询结果", MetadataJson: `{"tool_name":"order_get","status":"success"}`, ClientMessageId: "client-1", CreatedAt: "2026-08-20T15:00:02+08:00"},
		},
	}}
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(42))
	l := NewHistoryLogic(ctx, &svc.ServiceContext{AiAgentRpc: rpc})

	resp, err := l.ListMessages(&types.ListMessagesRequest{ConversationID: "conv-1", Page: 2, PageSize: 50})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if rpc.listMessagesReq == nil || rpc.listMessagesReq.UserId != 42 || rpc.listMessagesReq.ConversationId != "conv-1" || rpc.listMessagesReq.Page != 2 || rpc.listMessagesReq.PageSize != 50 {
		t.Fatalf("rpc request = %+v", rpc.listMessagesReq)
	}
	if resp.ConversationID != "conv-1" || resp.Total != 3 || len(resp.Messages) != 3 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Messages[0].Role != "user" || resp.Messages[1].Role != "assistant" || resp.Messages[2].Role != "tool" {
		t.Fatalf("roles = %q, %q, %q", resp.Messages[0].Role, resp.Messages[1].Role, resp.Messages[2].Role)
	}
	tool := resp.Messages[2]
	if tool.MessageID != "msg-3" || tool.ClientMessageID != "client-1" {
		t.Fatalf("tool message = %+v", tool)
	}
	if string(tool.Metadata) != `{"tool_name":"order_get","status":"success"}` {
		t.Fatalf("tool metadata = %s", tool.Metadata)
	}
	var meta map[string]string
	if err := json.Unmarshal(tool.Metadata, &meta); err != nil || meta["tool_name"] != "order_get" {
		t.Fatalf("tool metadata unmarshal = %v, %+v", err, meta)
	}
}

func TestListMessagesOmitsInvalidMetadataJSON(t *testing.T) {
	rpc := &fakeAiAgent{listMessagesResp: &aiagent.ListMessagesResponse{
		ConversationId: "conv-1",
		Messages: []*aiagent.HistoryMessage{{
			MessageId:    "msg-1",
			Role:         "assistant",
			Content:      "ok",
			MetadataJson: "not-json",
			CreatedAt:    "2026-08-20T15:00:00+08:00",
		}},
	}}
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(42))
	l := NewHistoryLogic(ctx, &svc.ServiceContext{AiAgentRpc: rpc})

	resp, err := l.ListMessages(&types.ListMessagesRequest{ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if resp.Messages[0].Metadata != nil {
		t.Fatalf("invalid metadata should be omitted, got %s", resp.Messages[0].Metadata)
	}
}

func TestListMessagesMapsRPCErrorToServerError(t *testing.T) {
	rpc := &fakeAiAgent{listMessagesErr: errors.New("rpc down")}
	ctx := context.WithValue(context.Background(), biz.UserIDKey, uint32(42))
	l := NewHistoryLogic(ctx, &svc.ServiceContext{AiAgentRpc: rpc})

	_, err := l.ListMessages(&types.ListMessagesRequest{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), code.ServerErrorMsg) {
		t.Fatalf("ListMessages error = %v, want server error", err)
	}
}
