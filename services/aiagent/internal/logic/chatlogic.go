package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/metadata"
)

type ChatLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

type agentEventSender interface {
	Send(*aiagent.AgentEvent) error
	Context() context.Context
}

func NewChatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChatLogic {
	return &ChatLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Chat 处理用户的聊天请求，并按 AgentEvent 级别流式返回。
func (l *ChatLogic) Chat(in *aiagent.ChatRequest, stream agentEventSender) error {
	if stream == nil {
		return errors.New("stream 为空")
	}
	// 参数校验
	if err := l.validateRequest(in); err != nil {
		return sendErrorEvent(stream, "", err)
	}
	if l.svcCtx == nil || l.svcCtx.ConversationManager == nil || l.svcCtx.ContextManager == nil || l.svcCtx.AgentRunner == nil || l.svcCtx.MessagesModel == nil {
		return sendErrorEvent(stream, in.ConversationId, errors.New("AI 服务暂时不可用，请稍后重试"))
	}
	source := in.Source
	if source == "" {
		source = "web"
	}
	metadata, _ := json.Marshal(map[string]string{"source": source})
	// 保存用户消息、幂等控制、新会话初始化
	prepared, err := l.svcCtx.ConversationManager.Prepare(l.ctx, conversation.PrepareRequest{
		UserID: uint64(in.UserId), ConversationID: in.ConversationId, MessageID: in.MessageId, ClientMessageID: in.ClientMessageId,
		Content: strings.TrimSpace(in.Content), Metadata: sql.NullString{String: string(metadata), Valid: true},
	})
	if err != nil || prepared == nil {
		if err == nil {
			err = errors.New("会话初始化失败")
		}
		return sendErrorEvent(stream, in.ConversationId, err)
	}
	if prepared.Duplicate {
		return l.replayDuplicateResponse(stream, prepared, uint64(in.UserId))
	}
	currentInput := strings.TrimSpace(in.Content)
	agentContext, err := l.svcCtx.ContextManager.Build(l.ctx, domain.BuildContextRequest{
		UserID:           uint64(in.UserId),
		ConversationID:   prepared.ConversationID,
		Mode:             domain.AgentContextMode,
		CurrentMessageID: prepared.UserMessageID,
		CurrentInput:     currentInput,
	})
	if err != nil || agentContext == nil {
		if err == nil {
			err = errors.New("对话上下文构建失败")
		}
		return sendErrorEvent(stream, prepared.ConversationID, err)
	}

	persistedMessages, err := l.runSupervisor(in, prepared, agentContext.Messages, stream)
	if err != nil {
		return err
	}
	go l.publishProfileUpdate(prepared, persistedMessages, uint64(in.UserId))
	go l.refreshConversationSummary(prepared.ConversationID, uint64(in.UserId))
	return nil
}

// publishProfileUpdate 发布用户画像更新事件
func (l *ChatLogic) publishProfileUpdate(prepared *conversation.PreparedConversation, messages []*aimessages.AiMessages, userID uint64) {
	if l.svcCtx.ProfileUpdatePublisher == nil || prepared == nil {
		return
	}
	messageIDs := make([]string, 0, len(messages)+1)
	// 传当前用户消息
	if prepared.UserMessageID != "" {
		messageIDs = append(messageIDs, prepared.UserMessageID)
	}
	// 传其他消息
	for _, message := range messages {
		if message != nil && message.MsgId != "" {
			messageIDs = append(messageIDs, message.MsgId)
		}
	}
	event := profileextractor.UpdateEvent{
		EventID:        "profile_evt_" + uuid.NewString(),
		UserID:         userID,
		ConversationID: prepared.ConversationID,
		MessageIDs:     messageIDs,
		CreatedAt:      time.Now(),
	}
	// 推到kafka
	if err := l.svcCtx.ProfileUpdatePublisher.PublishProfileUpdate(l.ctx, event); err != nil {
		l.Errorw("publish ai user profile update failed",
			logx.Field("component", "profile_extractor"),
			logx.Field("stage", "publish_update_event"),
			logx.Field("conversation_id", prepared.ConversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err))
	}
}

// replayDuplicateResponse 重放重复请求的响应
func (l *ChatLogic) replayDuplicateResponse(stream agentEventSender, prepared *conversation.PreparedConversation, userID uint64) error {
	rows, err := l.svcCtx.MessagesModel.FindAssistantMessagesByClientMessageID(l.ctx, userID, prepared.ConversationID, prepared.ClientMessageID)
	if err != nil {
		return sendErrorEvent(stream, prepared.ConversationID, err)
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		if err := stream.Send(&aiagent.AgentEvent{
			Type:           domain.EventAssistantMessage,
			ConversationId: row.ConversationId,
			MessageId:      row.MsgId,
			Content:        row.Content,
			Done:           true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// refreshConversationSummary 尝试刷新会话的滚动摘要
func (l *ChatLogic) refreshConversationSummary(conversationID string, userID uint64) {
	if l.svcCtx == nil || l.svcCtx.SummaryManager == nil {
		return
	}
	if _, err := l.svcCtx.SummaryManager.MaybeRefresh(l.ctx, contextmanager.SummaryRefreshRequest{
		UserID: userID, ConversationID: conversationID,
	}); err != nil {
		l.Errorw("refresh ai conversation summary failed",
			logx.Field("component", "context_manager"),
			logx.Field("stage", "summary_refresh"),
			logx.Field("conversation_id", conversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err))
	}
}

// persistenceErrorEvent 生成持久化消息失败的事件
func persistenceErrorEvent(conversationID string, businessExecuted bool) *aiagent.AgentEvent {
	content := "消息保存失败，请稍后重试"
	dataJSON := ""
	if businessExecuted {
		content = "业务结果已产生，但消息保存失败，请勿重复操作"
		dataJSON = `{"business_executed":true}`
	}
	return agentEventToProto(domain.AgentEvent{Type: domain.EventError, ConversationID: conversationID, MessageID: newChatMessageID(), Content: content, Status: "failed", DataJSON: dataJSON, Done: true})
}

func (l *ChatLogic) runSupervisor(in *aiagent.ChatRequest, prepared *conversation.PreparedConversation, agentMessages []domain.ContextMessage, stream agentEventSender) ([]*aimessages.AiMessages, error) {
	persistedMessages := make([]*aimessages.AiMessages, 0, 2)
	eventStream, err := l.svcCtx.AgentRunner.Stream(l.ctx, eino.RunRequest{
		UserID:         uint64(in.UserId),
		ConversationID: prepared.ConversationID,
		MessageID:      newChatMessageID(),
		ClientIP:       clientIPFromContext(l.ctx),
		Messages:       agentMessages,
	})
	if err != nil {
		l.Errorw("ai supervisor execution failed", logx.Field("component", "supervisor_agent"), logx.Field("stage", "execute"), logx.Field("reason", eino.ErrorReason(err)), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", err))
		if sendErr := stream.Send(&aiagent.AgentEvent{Type: domain.EventError, ConversationId: prepared.ConversationID, MessageId: newChatMessageID(), Content: "AI 服务暂时不可用，请稍后重试", Done: true}); sendErr != nil {
			return persistedMessages, sendErr
		}
		return persistedMessages, nil
	}
	events := 0
	forwardState := newEventForwardState()
	for event := range eventStream {
		events++
		normalizeAgentEvent(&event, prepared)
		forward := forwardState.shouldForward(event)
		if isTransientAgentEvent(event.Type) {
			if !forward {
				continue
			}
			if err := stream.Send(agentEventToProto(event)); err != nil {
				return persistedMessages, err
			}
			continue
		}
		message, err := agentEventToMessage(uint64(in.UserId), prepared.ClientMessageID, event)
		if err != nil {
			return persistedMessages, err
		}
		if err := l.svcCtx.MessagesModel.InsertBatch(l.ctx, []*aimessages.AiMessages{message}); err != nil {
			_ = stream.Send(persistenceErrorEvent(prepared.ConversationID, event.BusinessExecuted))
			return persistedMessages, err
		}
		persistedMessages = append(persistedMessages, message)
		if !forward {
			continue
		}
		if err := stream.Send(agentEventToProto(event)); err != nil {
			return persistedMessages, err
		}
	}
	if events == 0 {
		l.Errorw("ai supervisor returned no events", logx.Field("component", "supervisor_agent"), logx.Field("stage", "execute"), logx.Field("reason", "model_empty_response"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId))
		if sendErr := stream.Send(&aiagent.AgentEvent{Type: domain.EventError, ConversationId: prepared.ConversationID, MessageId: newChatMessageID(), Content: "AI 服务暂时不可用，请稍后重试", Done: true}); sendErr != nil {
			return persistedMessages, sendErr
		}
	}
	return persistedMessages, nil
}

func normalizeAgentEvent(event *domain.AgentEvent, prepared *conversation.PreparedConversation) {
	if event == nil || prepared == nil {
		return
	}
	if strings.TrimSpace(event.ConversationID) == "" {
		event.ConversationID = prepared.ConversationID
	}
	if strings.TrimSpace(event.MessageID) == "" || event.MessageID == prepared.UserMessageID {
		event.MessageID = newChatMessageID()
	}
}

// agentEventsToMessages 将事件转换为数据库消息记录格式
func agentEventsToMessages(userID uint64, clientMessageID string, events []domain.AgentEvent) ([]*aimessages.AiMessages, error) {
	messages := make([]*aimessages.AiMessages, 0, len(events))
	for _, event := range events {
		if !shouldPersistAgentEvent(event.Type) {
			continue
		}
		message, err := agentEventToMessage(userID, clientMessageID, event)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// agentEventToMessage 将事件转换为数据库消息记录格式
func agentEventToMessage(userID uint64, clientMessageID string, event domain.AgentEvent) (*aimessages.AiMessages, error) {
	role := conversation.RoleAssistant
	metadata := sql.NullString{}
	if event.Type == domain.EventToolResult || event.Type == domain.EventConfirmationRequired {
		role = conversation.RoleTool
		toolCallID := event.ToolCallID
		if toolCallID == "" {
			toolCallID = event.MessageID
		}
		raw, err := contextmanager.BuildToolResultMetadata(toolCallID, event.Tool, event.Status, event.ConfirmationID, event.DataJSON, event.Content)
		if err != nil {
			return nil, err
		}
		metadata = sql.NullString{String: raw, Valid: true}
	}
	return &aimessages.AiMessages{
		MsgId:           event.MessageID,
		ConversationId:  event.ConversationID,
		UserId:          userID,
		Role:            role,
		Content:         event.Content,
		Metadata:        metadata,
		ClientMessageId: sql.NullString{String: clientMessageID, Valid: strings.TrimSpace(clientMessageID) != ""},
		CreatedAt:       time.Now(),
	}, nil
}

// 是否是增量事件或者工具调用进度事件，这类事件不需要持久化
func isTransientAgentEvent(eventType string) bool {
	return eventType == domain.EventAssistantDelta || eventType == domain.EventToolProgress
}

func shouldPersistAgentEvent(eventType string) bool {
	return !isTransientAgentEvent(eventType)
}

type eventForwardState struct {
	assistantDeltaSent bool
}

func newEventForwardState() *eventForwardState {
	return &eventForwardState{}
}

func (s *eventForwardState) shouldForward(event domain.AgentEvent) bool {
	if s == nil {
		return true
	}
	switch event.Type {
	case domain.EventAssistantDelta:
		s.assistantDeltaSent = true
		return true
	case domain.EventAssistantMessage:
		return !s.assistantDeltaSent
	default:
		return true
	}
}

func (l *ChatLogic) validateRequest(in *aiagent.ChatRequest) error {
	if in == nil || in.UserId == 0 {
		return errors.New("用户身份无效")
	}
	if strings.TrimSpace(in.Content) == "" {
		return errors.New("消息内容不能为空")
	}
	if strings.TrimSpace(in.ClientMessageId) == "" {
		return errors.New("client_message_id 不能为空")
	}
	return nil
}

func sendErrorEvent(stream agentEventSender, conversationID string, err error) error {
	message := "AI 服务暂时不可用，请稍后重试"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return stream.Send(&aiagent.AgentEvent{Type: domain.EventError, ConversationId: conversationID, MessageId: newChatMessageID(), Content: message, Done: true})
}

func newChatMessageID() string {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return "msg_" + id.String()
}

func clientIPFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(biz.ClientIPKey).(string); ok {
		return value
	}
	if values := metadata.ValueFromIncomingContext(ctx, "x-client-ip"); len(values) > 0 {
		return values[0]
	}
	return ""
}
