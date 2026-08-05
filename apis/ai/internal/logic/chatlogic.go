package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/metadata"
)

const (
	maxSSERequestSize = 64 << 10
	sseIdleHeartbeat  = 10 * time.Second
	sseRequestTimeout = 5 * time.Minute
)

type ChatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

type agentEventReceiver interface {
	Recv() (*aiagent.AgentEvent, error)
}

type streamItem struct {
	event *aiagent.AgentEvent
	err   error
}

func NewChatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChatLogic {
	return &ChatLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *ChatLogic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(biz.UserIDKey).(uint32)
	if !ok || userID == 0 || l.svcCtx == nil || l.svcCtx.AiAgentRpc == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var input types.ClientMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSSERequestSize)).Decode(&input); err != nil {
		l.writeEvent(w, flusher, errorEvent("", "消息格式无效"))
		l.writeDone(w, flusher)
		return
	}
	if len(input.UserID) > 0 {
		l.writeEvent(w, flusher, errorEvent(strings.TrimSpace(input.ConversationID), "客户端不得提交 user_id"))
		l.writeDone(w, flusher)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sseRequestTimeout)
	defer cancel()
	if clientIP, ok := r.Context().Value(biz.ClientIPKey).(string); ok && strings.TrimSpace(clientIP) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-client-ip", clientIP)
	}

	conversationID := strings.TrimSpace(input.ConversationID)
	switch input.Type {
	case types.ClientEventUserMessage:
		l.handleUserMessage(ctx, w, flusher, userID, conversationID, input)
	case types.ClientEventConfirmAction:
		l.handleConfirmAction(ctx, w, flusher, userID, conversationID, input)
	default:
		l.writeEvent(w, flusher, errorEvent(conversationID, "不支持的消息类型"))
		l.writeDone(w, flusher)
	}
}

func (l *ChatLogic) handleUserMessage(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, userID uint32, conversationID string, input types.ClientMessage) {
	if strings.TrimSpace(input.Content) == "" {
		l.writeEvent(w, flusher, errorEvent(conversationID, "content 为必填字段"))
		l.writeDone(w, flusher)
		return
	}
	clientMessageID := strings.TrimSpace(input.ClientMessageID)
	if clientMessageID == "" {
		l.writeEvent(w, flusher, errorEvent(conversationID, "client_message_id 为必填字段"))
		l.writeDone(w, flusher)
		return
	}
	source := strings.TrimSpace(input.Metadata.Source)
	if source == "" {
		source = "web"
	}
	stream, err := l.svcCtx.AiAgentRpc.Chat(ctx, &aiagent.ChatRequest{
		UserId:          userID,
		ConversationId:  conversationID,
		MessageId:       "",
		Content:         input.Content,
		Source:          source,
		ClientMessageId: clientMessageID,
	})
	if err != nil || stream == nil {
		l.writeEvent(w, flusher, errorEvent(conversationID, "AI 服务暂时不可用，请稍后重试"))
		l.writeDone(w, flusher)
		return
	}
	l.proxyStream(ctx, w, flusher, stream, conversationID, "AI 服务暂时不可用，请稍后重试")
}

func (l *ChatLogic) handleConfirmAction(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, userID uint32, conversationID string, input types.ClientMessage) {
	if conversationID == "" || strings.TrimSpace(input.ConfirmationID) == "" || input.Approved == nil {
		l.writeEvent(w, flusher, errorEvent(conversationID, "conversation_id、confirmation_id 和 approved 为必填字段"))
		l.writeDone(w, flusher)
		return
	}
	stream, err := l.svcCtx.AiAgentRpc.ConfirmAction(ctx, &aiagent.ConfirmActionRequest{
		UserId:         userID,
		ConversationId: conversationID,
		ConfirmationId: strings.TrimSpace(input.ConfirmationID),
		Approved:       *input.Approved,
	})
	if err != nil || stream == nil {
		l.writeEvent(w, flusher, errorEvent(conversationID, "确认服务暂时不可用，请稍后重试"))
		l.writeDone(w, flusher)
		return
	}
	l.proxyStream(ctx, w, flusher, stream, conversationID, "确认服务暂时不可用，请稍后重试")
}

// sse流式代理函数, 将agent的事件流实时推给前端
func (l *ChatLogic) proxyStream(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, receiver agentEventReceiver, fallbackConversationID, rpcErrorMessage string) {
	items := make(chan streamItem, 1)
	// 启动一个goroutine接收agent事件流
	go func() {
		defer close(items)
		for {
			event, err := receiver.Recv()
			if err != nil {
				select {
				case items <- streamItem{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case items <- streamItem{event: event}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// 启动一个定时器，用于发送心跳包
	ticker := time.NewTicker(sseIdleHeartbeat)
	defer ticker.Stop()
	for {
		select {
		// 处理请求超时
		case <-ctx.Done():
			l.writeEvent(w, flusher, errorEvent(fallbackConversationID, "AI 服务请求超时，请稍后重试"))
			l.writeDone(w, flusher)
			return
		// 处理心跳
		case <-ticker.C:
			l.writeHeartbeat(w, flusher)
		// 处理agent事件流
		case item, ok := <-items:
			if !ok {
				l.writeDone(w, flusher)
				return
			}
			if item.err != nil {
				if errors.Is(item.err, io.EOF) {
					l.writeDone(w, flusher)
					return
				}
				if ctx.Err() != nil {
					l.writeEvent(w, flusher, errorEvent(fallbackConversationID, "AI 服务请求超时，请稍后重试"))
				} else {
					l.writeEvent(w, flusher, errorEvent(fallbackConversationID, rpcErrorMessage))
				}
				l.writeDone(w, flusher)
				return
			}
			mapped, err := mapAgentEvent(item.event)
			if err != nil {
				l.writeEvent(w, flusher, errorEvent(fallbackConversationID, err.Error()))
				continue
			}
			if mapped.ConversationID != "" {
				fallbackConversationID = mapped.ConversationID
			}
			l.writeEvent(w, flusher, mapped)
		}
	}
}

func mapAgentEvent(event *aiagent.AgentEvent) (types.ServerEvent, error) {
	if event == nil {
		return types.ServerEvent{}, errors.New("AI 服务返回空事件")
	}
	switch event.Type {
	case "assistant_message", "tool_result", "confirmation_required", "error":
	default:
		return types.ServerEvent{}, errors.New("AI 服务返回未知事件")
	}
	result := types.ServerEvent{Type: event.Type, ConversationID: event.ConversationId, MessageID: event.MessageId, Content: event.Content, Tool: event.Tool, Status: event.Status, ConfirmationID: event.ConfirmationId, Action: event.Action, Summary: event.Summary, ExpiresAt: event.ExpiresAt, Done: event.Done}
	if strings.TrimSpace(event.DataJson) != "" {
		if !json.Valid([]byte(event.DataJson)) {
			return types.ServerEvent{}, errors.New("AI 服务返回无效工具数据")
		}
		result.Data = json.RawMessage(event.DataJson)
	}
	return result, nil
}

func errorEvent(conversationID, content string) types.ServerEvent {
	return types.ServerEvent{Type: "error", ConversationID: conversationID, Content: content, Done: true}
}

func (l *ChatLogic) writeEvent(w http.ResponseWriter, flusher http.Flusher, event types.ServerEvent) {
	eventName := strings.TrimSpace(event.Type)
	if eventName == "" {
		eventName = "message"
	}
	payload, err := json.Marshal(event)
	if err != nil {
		eventName = "error"
		payload = []byte(`{"type":"error","content":"事件编码失败","done":true}`)
	}
	fmt.Fprintf(w, "event: %s\n", eventName)
	if strings.TrimSpace(event.MessageID) != "" {
		fmt.Fprintf(w, "id: %s\n", event.MessageID)
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func (l *ChatLogic) writeHeartbeat(w http.ResponseWriter, flusher http.Flusher) {
	fmt.Fprint(w, ": ping\n\n")
	// 用于立即将缓冲区的数据发送到客户端
	flusher.Flush()
}

func (l *ChatLogic) writeDone(w http.ResponseWriter, flusher http.Flusher) {
	fmt.Fprint(w, "event: done\n")
	fmt.Fprint(w, "data: {\"done\":true}\n\n")
	flusher.Flush()
}
