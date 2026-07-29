package logic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"google.golang.org/grpc"
)

func TestChatWebSocketForwardsTrustedUserAndEvents(t *testing.T) {
	rpc := &fakeAiAgent{chatResp: &aiagent.ChatResponse{Events: []*aiagent.AgentEvent{{Type: "assistant_message", ConversationId: "conv-1", MessageId: "msg-1", Content: "你好", Done: true}}}}
	server := websocketTestServer(rpc)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/douyin/ai/chat", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "user_message", "content": "你好", "client_message_id": "client-1"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var event types.ServerEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read: %v", err)
	}
	if rpc.chatReq == nil || rpc.chatReq.UserId != 42 || rpc.chatReq.Source != "web" || rpc.chatReq.MessageId != "" || rpc.chatReq.ClientMessageId != "client-1" {
		t.Fatalf("rpc request=%+v", rpc.chatReq)
	}
	if event.Type != "assistant_message" || event.ConversationID != "conv-1" || event.Content != "你好" {
		t.Fatalf("event=%+v", event)
	}
}

func TestChatWebSocketRejectsMissingClientMessageIDForUserMessage(t *testing.T) {
	rpc := &fakeAiAgent{}
	server := websocketTestServer(rpc)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/douyin/ai/chat", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "user_message", "content": "你好"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var event types.ServerEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read: %v", err)
	}
	if event.Type != "error" || rpc.chatReq != nil {
		t.Fatalf("event=%+v rpc=%+v", event, rpc.chatReq)
	}
}

func TestChatWebSocketRejectsPayloadUserID(t *testing.T) {
	rpc := &fakeAiAgent{}
	server := websocketTestServer(rpc)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/douyin/ai/chat", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "user_message", "message_id": "legacy-client-id", "client_message_id": "client-1", "content": "你好", "user_id": 999})
	var event types.ServerEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read: %v", err)
	}
	if event.Type != "error" || rpc.chatReq != nil {
		t.Fatalf("event=%+v rpc=%+v", event, rpc.chatReq)
	}
}

func TestChatWebSocketIgnoresLegacyMessageID(t *testing.T) {
	rpc := &fakeAiAgent{chatResp: &aiagent.ChatResponse{Events: []*aiagent.AgentEvent{{Type: "assistant_message", ConversationId: "conv-1", Content: "你好", Done: true}}}}
	server := websocketTestServer(rpc)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/douyin/ai/chat", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "user_message", "message_id": "legacy-client-id", "client_message_id": "client-1", "content": "你好"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var event types.ServerEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read: %v", err)
	}
	if rpc.chatReq == nil || rpc.chatReq.MessageId != "" || event.Type != "assistant_message" {
		t.Fatalf("rpc request=%+v event=%+v", rpc.chatReq, event)
	}
}

func websocketTestServer(rpc *fakeAiAgent) *httptest.Server {
	ctx := &svc.ServiceContext{AiAgentRpc: rpc}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), biz.UserIDKey, uint32(42)))
		NewChatLogic(r.Context(), ctx).ServeHTTP(w, r, &types.ChatRequest{})
	}))
}

type fakeAiAgent struct {
	chatReq  *aiagent.ChatRequest
	chatResp *aiagent.ChatResponse
}

func (f *fakeAiAgent) Chat(_ context.Context, req *aiagent.ChatRequest, _ ...grpc.CallOption) (*aiagent.ChatResponse, error) {
	f.chatReq = req
	return f.chatResp, nil
}

func (f *fakeAiAgent) ConfirmAction(context.Context, *aiagent.ConfirmActionRequest, ...grpc.CallOption) (*aiagent.ConfirmActionResponse, error) {
	return &aiagent.ConfirmActionResponse{}, nil
}
