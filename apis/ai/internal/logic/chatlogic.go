package logic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/metadata"
)

const (
	maxWebSocketMessageSize = 64 << 10         // 64KB 消息大小限制
	webSocketWriteTimeout   = 10 * time.Second // 写入超时时间
)

var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

type ChatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChatLogic {
	return &ChatLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *ChatLogic) ServeHTTP(w http.ResponseWriter, r *http.Request, req *types.ChatRequest) {
	// 从上下文获取用户 ID
	userID, ok := r.Context().Value(biz.UserIDKey).(uint32)
	if !ok || userID == 0 || l.svcCtx == nil || l.svcCtx.AiAgentRpc == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// 升级 HTTP 连接为 WebSocket 连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		l.Errorf("upgrade websocket: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxWebSocketMessageSize)
	conversationID := strings.TrimSpace(req.ConversationId)
	// 持续监听消息
	for {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			if !websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				l.Infof("websocket closed: %v", readErr)
			}
			return
		}
		if messageType != websocket.TextMessage {
			l.writeEvent(conn, errorEvent(conversationID, "仅支持 JSON 文本消息"))
			continue
		}
		// 处理消息
		events, nextConversationID := l.handleMessage(r.Context(), userID, conversationID, payload)
		// 将事件发送回客户端
		for _, event := range events {
			if err := l.writeEvent(conn, event); err != nil {
				return
			}
		}
		if nextConversationID != "" {
			conversationID = nextConversationID
		}
	}
}

// handleMessage 处理客户端发送的消息，并返回生成的事件列表和可能更新的会话 ID
func (l *ChatLogic) handleMessage(ctx context.Context, userID uint32, conversationID string, payload []byte) ([]types.ServerEvent, string) {
	// 从上下文获取客户端 IP 并添加到 gRPC 上下文
	if clientIP, ok := ctx.Value(biz.ClientIPKey).(string); ok && strings.TrimSpace(clientIP) != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-client-ip", clientIP)
	}
	var input types.ClientMessage
	if err := json.Unmarshal(payload, &input); err != nil {
		return []types.ServerEvent{errorEvent(conversationID, "消息格式无效")}, ""
	}
	if len(input.UserID) > 0 {
		return []types.ServerEvent{errorEvent(conversationID, "客户端不得提交 user_id")}, ""
	}
	var events []*aiagent.AgentEvent
	var statusCode int32
	var statusMessage string
	// 根据消息类型分发处理
	switch input.Type {
	// 用户消息类型
	case types.ClientEventUserMessage:
		if strings.TrimSpace(input.MessageID) == "" || strings.TrimSpace(input.Content) == "" {
			return []types.ServerEvent{errorEvent(conversationID, "message_id 和 content 为必填字段")}, ""
		}
		source := strings.TrimSpace(input.Metadata.Source)
		if source == "" {
			source = "web"
		}
		resp, err := l.svcCtx.AiAgentRpc.Chat(ctx, &aiagent.ChatRequest{UserId: userID, ConversationId: conversationID, MessageId: input.MessageID, Content: input.Content, Source: source})
		if err != nil || resp == nil {
			return []types.ServerEvent{errorEvent(conversationID, "AI 服务暂时不可用，请稍后重试")}, ""
		}
		events, statusCode, statusMessage = resp.Events, resp.StatusCode, resp.StatusMsg
	// 确认操作消息类型
	case types.ClientEventConfirmAction:
		if strings.TrimSpace(conversationID) == "" || strings.TrimSpace(input.ConfirmationID) == "" || input.Approved == nil {
			return []types.ServerEvent{errorEvent(conversationID, "conversation_id、confirmation_id 和 approved 为必填字段")}, ""
		}
		resp, err := l.svcCtx.AiAgentRpc.ConfirmAction(ctx, &aiagent.ConfirmActionRequest{UserId: userID, ConversationId: conversationID, ConfirmationId: input.ConfirmationID, Approved: *input.Approved})
		if err != nil || resp == nil {
			return []types.ServerEvent{errorEvent(conversationID, "确认服务暂时不可用，请稍后重试")}, ""
		}
		events, statusCode, statusMessage = resp.Events, resp.StatusCode, resp.StatusMsg
	default:
		return []types.ServerEvent{errorEvent(conversationID, "不支持的消息类型")}, ""
	}
	if len(events) == 0 {
		message := "AI 服务未返回有效事件"
		if statusCode != 0 && strings.TrimSpace(statusMessage) != "" {
			message = statusMessage
		}
		return []types.ServerEvent{errorEvent(conversationID, message)}, ""
	}
	output := make([]types.ServerEvent, 0, len(events))
	nextConversationID := ""
	for _, event := range events {
		mapped, err := mapAgentEvent(event)
		if err != nil {
			output = append(output, errorEvent(conversationID, err.Error()))
			continue
		}
		output = append(output, mapped)
		if mapped.ConversationID != "" {
			nextConversationID = mapped.ConversationID
		}
	}
	return output, nextConversationID
}

// mapAgentEvent 将 aiagent.AgentEvent 转换为 types.ServerEvent
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

// writeEvent 将事件发送回客户端
func (l *ChatLogic) writeEvent(conn *websocket.Conn, event types.ServerEvent) error {
	_ = conn.SetWriteDeadline(time.Now().Add(webSocketWriteTimeout))
	return conn.WriteJSON(event)
}
