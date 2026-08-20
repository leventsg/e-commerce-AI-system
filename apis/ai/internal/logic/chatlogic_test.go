package logic

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestChatSSEForwardsTrustedUserConversationAndEvents(t *testing.T) {
	rpc := &fakeAiAgent{chatEvents: []*aiagent.AgentEvent{{
		Type:           "assistant_message",
		ConversationId: "conv-body",
		MessageId:      "msg-1",
		Content:        "你好",
		Done:           true,
	}}}
	server := sseTestServer(rpc)
	defer server.Close()

	resp := postSSE(t, server.URL+"/douyin/ai/chat", `{
		"type":"user_message",
		"conversation_id":"conv-body",
		"content":"你好",
		"client_message_id":"client-1",
		"metadata":{"source":"web"}
	}`)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	event := readFirstDataEvent(t, resp.Body)
	if rpc.chatReq == nil || rpc.chatReq.UserId != 42 || rpc.chatReq.ConversationId != "conv-body" || rpc.chatReq.Source != "web" || rpc.chatReq.ClientMessageId != "client-1" {
		t.Fatalf("rpc request=%+v", rpc.chatReq)
	}
	if event.Type != "assistant_message" || event.ConversationID != "conv-body" || event.Content != "你好" {
		t.Fatalf("event=%+v", event)
	}
}

func TestChatSSEForwardsAuthTokensToAgentRPC(t *testing.T) {
	rpc := &fakeAiAgent{chatEvents: []*aiagent.AgentEvent{{
		Type:           "assistant_message",
		ConversationId: "conv-1",
		MessageId:      "msg-1",
		Content:        "ok",
		Done:           true,
	}}}
	server := sseTestServer(rpc)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/douyin/ai/chat", strings.NewReader(`{
		"type":"user_message",
		"conversation_id":"conv-1",
		"content":"hello",
		"client_message_id":"client-1"
	}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(biz.TokenKey, "access-token")
	req.AddCookie(&http.Cookie{Name: biz.RefreshTokenKey, Value: "refresh-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	_ = readFirstDataEvent(t, resp.Body)

	if rpc.chatCtx == nil {
		t.Fatal("chat ctx not captured")
	}
	md, ok := metadata.FromOutgoingContext(rpc.chatCtx)
	if !ok {
		t.Fatal("outgoing metadata missing")
	}
	if got := md.Get("x-access-token"); len(got) != 1 || got[0] != "access-token" {
		t.Fatalf("access token metadata=%v", got)
	}
	if got := md.Get("x-refresh-token"); len(got) != 1 || got[0] != "refresh-token" {
		t.Fatalf("refresh token metadata=%v", got)
	}
}

func TestChatSSERejectsMissingClientMessageIDForUserMessage(t *testing.T) {
	rpc := &fakeAiAgent{}
	server := sseTestServer(rpc)
	defer server.Close()

	resp := postSSE(t, server.URL+"/douyin/ai/chat", `{"type":"user_message","content":"你好"}`)
	defer resp.Body.Close()

	event := readFirstDataEvent(t, resp.Body)
	if event.Type != "error" || rpc.chatReq != nil {
		t.Fatalf("event=%+v rpc=%+v", event, rpc.chatReq)
	}
}

func TestChatSSERejectsPayloadUserID(t *testing.T) {
	rpc := &fakeAiAgent{}
	server := sseTestServer(rpc)
	defer server.Close()

	resp := postSSE(t, server.URL+"/douyin/ai/chat", `{"type":"user_message","client_message_id":"client-1","content":"你好","user_id":999}`)
	defer resp.Body.Close()

	event := readFirstDataEvent(t, resp.Body)
	if event.Type != "error" || rpc.chatReq != nil {
		t.Fatalf("event=%+v rpc=%+v", event, rpc.chatReq)
	}
}

func TestChatSSEConfirmActionForwardsConfirmation(t *testing.T) {
	rpc := &fakeAiAgent{confirmEvents: []*aiagent.AgentEvent{{
		Type:           "tool_result",
		ConversationId: "conv-1",
		MessageId:      "msg-2",
		Tool:           "order_cancel",
		Status:         "success",
		Content:        "订单已取消。",
		Done:           true,
	}}}
	server := sseTestServer(rpc)
	defer server.Close()

	resp := postSSE(t, server.URL+"/douyin/ai/chat", `{"type":"confirm_action","conversation_id":"conv-1","confirmation_id":"confirm-1","approved":true}`)
	defer resp.Body.Close()

	event := readFirstDataEvent(t, resp.Body)
	if rpc.confirmReq == nil || rpc.confirmReq.UserId != 42 || rpc.confirmReq.ConversationId != "conv-1" || rpc.confirmReq.ConfirmationId != "confirm-1" || !rpc.confirmReq.Approved {
		t.Fatalf("confirm request=%+v", rpc.confirmReq)
	}
	if event.Type != "tool_result" || event.Tool != "order_cancel" {
		t.Fatalf("event=%+v", event)
	}
}

func TestMapAgentEventKeepsConfirmationID(t *testing.T) {
	event, err := mapAgentEvent(&aiagent.AgentEvent{
		Type:           "confirmation_required",
		ConversationId: "conv-1",
		MessageId:      "msg-1",
		ConfirmationId: "confirm-1",
		Action:         "order_cancel",
		Summary:        "确认取消订单？",
		ExpiresAt:      1893456000,
		DataJson:       `{"confirmation_id":"confirm-1"}`,
		Done:           true,
	})
	if err != nil {
		t.Fatalf("mapAgentEvent: %v", err)
	}
	if event.ConfirmationID != "confirm-1" || event.Action != "order_cancel" {
		t.Fatalf("event=%+v, want confirmation_id mapped", event)
	}
}

func TestMapAgentEventKeepsRAGSources(t *testing.T) {
	event, err := mapAgentEvent(&aiagent.AgentEvent{
		Type:           "assistant_message",
		ConversationId: "conv-1",
		MessageId:      "msg-1",
		Content:        "根据知识库回答",
		DataJson:       `{"sources":[{"document_id":"doc-1","title":"规范.md","document_url":"http://kb/preview/doc/doc-1","chunks":[{"chunk_id":"chunk-1","content":"知识内容","score":0.9}]}]}`,
		Done:           true,
	})
	if err != nil {
		t.Fatalf("mapAgentEvent: %v", err)
	}
	if len(event.Sources) != 1 || event.Sources[0].DocumentID != "doc-1" || len(event.Sources[0].Chunks) != 1 {
		t.Fatalf("sources=%+v", event.Sources)
	}
}

func TestMapAgentEventAllowsStreamingDeltaAndToolProgress(t *testing.T) {
	delta, err := mapAgentEvent(&aiagent.AgentEvent{
		Type:           "assistant_delta",
		ConversationId: "conv-1",
		MessageId:      "msg-delta",
		Content:        "你",
		Done:           false,
	})
	if err != nil {
		t.Fatalf("map assistant_delta: %v", err)
	}
	if delta.Type != "assistant_delta" || delta.Content != "你" || delta.Done {
		t.Fatalf("delta=%+v", delta)
	}

	progress, err := mapAgentEvent(&aiagent.AgentEvent{
		Type:           "tool_progress",
		ConversationId: "conv-1",
		MessageId:      "msg-progress",
		Tool:           "product_search",
		Status:         "running",
		Content:        "正在查询商品...",
		Done:           false,
	})
	if err != nil {
		t.Fatalf("map tool_progress: %v", err)
	}
	if progress.Type != "tool_progress" || progress.Tool != "product_search" || progress.Content == "" {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestMapAgentEventAllowsAssistantThinkingDelta(t *testing.T) {
	event, err := mapAgentEvent(&aiagent.AgentEvent{
		Type:           "assistant_thinking_delta",
		ConversationId: "conv-1",
		Content:        "我先分析一下",
		Done:           false,
	})
	if err != nil {
		t.Fatalf("map assistant_thinking_delta: %v", err)
	}
	if event.Type != "assistant_thinking_delta" || event.Content != "我先分析一下" || event.Done {
		t.Fatalf("event=%+v, want thinking delta mapped", event)
	}
	if event.MessageID != "" {
		t.Fatalf("event message_id = %q, want empty", event.MessageID)
	}
}

func sseTestServer(rpc *fakeAiAgent) *httptest.Server {
	ctx := &svc.ServiceContext{AiAgentRpc: rpc}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), biz.UserIDKey, uint32(42)))
		NewChatLogic(r.Context(), ctx).ServeHTTP(w, r)
	}))
}

func postSSE(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	return resp
}

func readFirstDataEvent(t *testing.T, body io.Reader) types.ServerEvent {
	t.Helper()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var event types.ServerEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatalf("decode data event: %v", err)
			}
			return event
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan sse: %v", err)
	}
	t.Fatal("no data event")
	return types.ServerEvent{}
}

type fakeAiAgent struct {
	chatReq       *aiagent.ChatRequest
	confirmReq    *aiagent.ConfirmActionRequest
	chatCtx       context.Context
	confirmCtx    context.Context
	chatEvents    []*aiagent.AgentEvent
	confirmEvents []*aiagent.AgentEvent
}

func (f *fakeAiAgent) Chat(ctx context.Context, req *aiagent.ChatRequest, _ ...grpc.CallOption) (aiagent.AiAgent_ChatClient, error) {
	f.chatReq = req
	f.chatCtx = ctx
	return &fakeAgentEventClient{events: f.chatEvents}, nil
}

func (f *fakeAiAgent) ConfirmAction(ctx context.Context, req *aiagent.ConfirmActionRequest, _ ...grpc.CallOption) (aiagent.AiAgent_ConfirmActionClient, error) {
	f.confirmReq = req
	f.confirmCtx = ctx
	return &fakeAgentEventClient{events: f.confirmEvents}, nil
}

type fakeAgentEventClient struct {
	grpc.ClientStream
	events []*aiagent.AgentEvent
	index  int
}

func (c *fakeAgentEventClient) Recv() (*aiagent.AgentEvent, error) {
	if c.index >= len(c.events) {
		return nil, io.EOF
	}
	event := c.events[c.index]
	c.index++
	return event, nil
}
