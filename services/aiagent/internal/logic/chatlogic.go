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
	memory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
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
	if l.svcCtx == nil || l.svcCtx.ConversationManager == nil || l.svcCtx.AgentRunner == nil || l.svcCtx.MessagesModel == nil {
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
	persistedMessages, err := l.runSupervisor(in, prepared, []domain.ContextMessage{
		{Role: domain.ContextRoleUser, Content: currentInput},
	}, stream)
	if err != nil {
		return err
	}
	go l.memorizeTurn(prepared, currentInput, persistedMessages, uint64(in.UserId))
	return nil
}

func (l *ChatLogic) memorizeTurn(prepared *conversation.PreparedConversation, currentInput string, messages []*aimessages.AiMessages, userID uint64) {
	if prepared == nil {
		return
	}
	finalAssistant := latestAssistantMessage(messages)
	if finalAssistant == nil {
		return
	}
	messageIDs := []string{prepared.UserMessageID, finalAssistant.MsgId}
	if l.svcCtx != nil && l.svcCtx.MemoryProvider != nil {
		if err := l.svcCtx.MemoryProvider.Memorize(l.ctx, &memory.MemorizeRequest{
			UserID:          userID,
			ConversationID:  prepared.ConversationID,
			ClientMessageID: prepared.ClientMessageID,
			Messages: []domain.ContextMessage{
				{Role: domain.ContextRoleUser, Content: currentInput},
				{Role: domain.ContextRoleAssistant, Content: finalAssistant.Content},
			},
			MessageIDs: messageIDs,
		}); err != nil {
			l.Errorw("memorize ai turn failed",
				logx.Field("component", "memory_provider"),
				logx.Field("stage", "memorize"),
				logx.Field("conversation_id", prepared.ConversationID),
				logx.Field("user_id", userID),
				logx.Field("err", err))
		}
		return
	}
	go l.publishProfileUpdate(prepared, messages, userID)
	go l.refreshConversationSummary(prepared.ConversationID, userID)
}

func latestAssistantMessage(messages []*aimessages.AiMessages) *aimessages.AiMessages {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == conversation.RoleAssistant && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i]
		}
	}
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
		UserID:           uint64(in.UserId),
		ConversationID:   prepared.ConversationID,
		MessageID:        newChatMessageID(),
		ClientIP:         clientIPFromContext(l.ctx),
		Messages:         agentMessages,
		CurrentMessageID: prepared.UserMessageID,
		ClientMessageID:  prepared.ClientMessageID,
	})
	if err != nil {
		l.Errorw("ai supervisor execution failed", logx.Field("component", "supervisor_agent"), logx.Field("stage", "execute"), logx.Field("reason", eino.ErrorReason(err)), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", err))
		if sendErr := stream.Send(&aiagent.AgentEvent{Type: domain.EventError, ConversationId: prepared.ConversationID, MessageId: newChatMessageID(), Content: "AI 服务暂时不可用，请稍后重试", Done: true}); sendErr != nil {
			return persistedMessages, sendErr
		}
		return persistedMessages, nil
	}
	events := 0
	businessExecuted := false
	// 消费run stream通道
	for event := range eventStream {
		events++
		// 处理需要持久化的事件
		if shouldPersistAgentEvent(event.Type) {
			message, err := agentEventToMessage(uint64(in.UserId), prepared.ClientMessageID, event)
			if err != nil {
				return persistedMessages, err
			}
			if event.BusinessExecuted {
				businessExecuted = true
			}
			// 保存消息到数据库
			result, err := l.svcCtx.MessagesModel.Insert(l.ctx, message)
			if err != nil {
				_ = stream.Send(persistenceErrorEvent(prepared.ConversationID, businessExecuted))
				return persistedMessages, err
			}
			if rows, err := result.RowsAffected(); err != nil || rows == 0 {
				l.Errorw("insert message failed", logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", err))
			}
			persistedMessages = append(persistedMessages, message)
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

func shouldPersistAgentEvent(eventType string) bool {
	switch eventType {
	case domain.EventAssistantMessage, domain.EventToolResult, domain.EventConfirmationRequired, domain.EventError:
		return true
	default:
		return false
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
