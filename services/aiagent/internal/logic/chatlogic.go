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
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
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

func NewChatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChatLogic {
	return &ChatLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Chat 处理用户的聊天请求，返回 AI 生成的响应事件
func (l *ChatLogic) Chat(in *aiagent.ChatRequest) (*aiagent.ChatResponse, error) {
	// 参数校验
	if err := l.validateRequest(in); err != nil {
		return chatErrorResponse("", err), nil
	}
	if l.svcCtx == nil || l.svcCtx.ConversationManager == nil || l.svcCtx.ContextManager == nil || l.svcCtx.AgentRunner == nil || l.svcCtx.MessagesModel == nil {
		return chatErrorResponse(in.ConversationId, errors.New("AI 服务暂时不可用，请稍后重试")), nil
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
		return chatErrorResponse(in.ConversationId, err), nil
	}
	if prepared.Duplicate {
		return l.replayDuplicateResponse(prepared, uint64(in.UserId))
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
		return chatErrorResponse(prepared.ConversationID, err), nil
	}

	events, persistedMessages := l.runSupervisor(in, prepared, agentContext.Messages)
	protoEvents := make([]*aiagent.AgentEvent, 0, len(events))
	businessExecuted := false
	for i := range events {
		normalizeAgentEvent(&events[i], prepared)
		if events[i].BusinessExecuted {
			businessExecuted = true
		}
		protoEvents = append(protoEvents, agentEventToProto(events[i]))
	}
	messages := persistedMessages
	if len(persistedMessages) == 0 {
		var err error
		// 将事件转换为数据库消息记录格式
		messages, err = agentEventsToMessages(uint64(in.UserId), prepared.ClientMessageID, events)
		if err != nil {
			protoEvents = append(protoEvents, persistenceErrorEvent(prepared.ConversationID, businessExecuted))
			return &aiagent.ChatResponse{StatusCode: code.ServerError, StatusMsg: err.Error(), Events: protoEvents}, nil
		}
		// 批量插入消息记录
		if err := l.svcCtx.MessagesModel.InsertBatch(l.ctx, messages); err != nil {
			protoEvents = append(protoEvents, persistenceErrorEvent(prepared.ConversationID, businessExecuted))
			return &aiagent.ChatResponse{StatusCode: code.ServerError, StatusMsg: err.Error(), Events: protoEvents}, nil
		}
	}
	// 异步更新用户画像
	l.publishProfileUpdate(prepared, messages, uint64(in.UserId))
	l.Logger.Info("即将执行摘要压缩")
	l.refreshConversationSummary(prepared.ConversationID, uint64(in.UserId))
	return &aiagent.ChatResponse{StatusCode: code.Success, StatusMsg: code.SuccessMsg, Events: protoEvents}, nil
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
func (l *ChatLogic) replayDuplicateResponse(prepared *conversation.PreparedConversation, userID uint64) (*aiagent.ChatResponse, error) {
	rows, err := l.svcCtx.MessagesModel.FindAssistantMessagesByClientMessageID(l.ctx, userID, prepared.ConversationID, prepared.ClientMessageID)
	if err != nil {
		return chatErrorResponse(prepared.ConversationID, err), nil
	}
	events := make([]*aiagent.AgentEvent, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		events = append(events, &aiagent.AgentEvent{
			Type:           domain.EventAssistantMessage,
			ConversationId: row.ConversationId,
			MessageId:      row.MsgId,
			Content:        row.Content,
			Done:           true,
		})
	}
	return &aiagent.ChatResponse{StatusCode: code.Success, StatusMsg: code.SuccessMsg, Events: events}, nil
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

func (l *ChatLogic) runSupervisor(in *aiagent.ChatRequest, prepared *conversation.PreparedConversation, agentMessages []domain.ContextMessage) ([]domain.AgentEvent, []*aimessages.AiMessages) {
	persistedMessages := make([]*aimessages.AiMessages, 0, 2)
	events, err := l.svcCtx.AgentRunner.Run(l.ctx, eino.RunRequest{
		UserID:         uint64(in.UserId),
		ConversationID: prepared.ConversationID,
		MessageID:      newChatMessageID(),
		ClientIP:       clientIPFromContext(l.ctx),
		Messages:       agentMessages,
		OnEvent: func(ctx context.Context, event domain.AgentEvent) error {
			normalizeAgentEvent(&event, prepared)
			message, err := agentEventToMessage(uint64(in.UserId), prepared.ClientMessageID, event)
			if err != nil {
				return err
			}
			if err := l.svcCtx.MessagesModel.InsertBatch(ctx, []*aimessages.AiMessages{message}); err != nil {
				return err
			}
			persistedMessages = append(persistedMessages, message)
			return nil
		},
	})
	if err != nil {
		l.Errorw("ai supervisor execution failed", logx.Field("component", "supervisor_agent"), logx.Field("stage", "execute"), logx.Field("reason", eino.ErrorReason(err)), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", err))
		return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "AI 服务暂时不可用，请稍后重试", Done: true}}, nil
	}
	if len(events) == 0 {
		l.Errorw("ai supervisor returned no events", logx.Field("component", "supervisor_agent"), logx.Field("stage", "execute"), logx.Field("reason", "model_empty_response"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId))
		return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "AI 服务暂时不可用，请稍后重试", Done: true}}, nil
	}
	return events, persistedMessages
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

func chatErrorResponse(conversationID string, err error) *aiagent.ChatResponse {
	message := "AI 服务暂时不可用，请稍后重试"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return &aiagent.ChatResponse{StatusCode: code.ServerError, StatusMsg: message, Events: []*aiagent.AgentEvent{{Type: domain.EventError, ConversationId: conversationID, MessageId: newChatMessageID(), Content: message, Done: true}}}
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
