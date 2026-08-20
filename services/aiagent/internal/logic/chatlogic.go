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
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/rag"
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
		l.Errorw("ai chat request invalid", logx.Field("component", "ai_chat_logic"), logx.Field("stage", "validate_request"), logx.Field("conversation_id", in.ConversationId), logx.Field("user_id", in.UserId), logx.Field("err", err))
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
		l.Errorw("ai conversation prepare failed", logx.Field("component", "ai_chat_logic"), logx.Field("stage", "conversation_prepare"), logx.Field("conversation_id", in.ConversationId), logx.Field("user_id", in.UserId), logx.Field("client_message_id", in.ClientMessageId), logx.Field("err", err))
		return sendErrorEvent(stream, in.ConversationId, err)
	}
	if prepared.Duplicate {
		l.Infow("ai chat duplicate replay", logx.Field("component", "ai_chat_logic"), logx.Field("stage", "duplicate_replay"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("client_message_id", prepared.ClientMessageID))
		return l.replayDuplicateResponse(stream, prepared, uint64(in.UserId))
	}
	var ragContext []domain.ContextMessage
	var ragSources []domain.AgentSource
	if l.svcCtx.RAGService != nil {
		ragResult, ragErr := l.svcCtx.RAGService.Prepare(l.ctx, rag.PrepareRequest{
			UserID:           uint64(in.UserId),
			ConversationID:   prepared.ConversationID,
			CurrentMessageID: prepared.UserMessageID,
			ClientMessageID:  prepared.ClientMessageID,
			Content:          rag.EmbeddingRequest{Query: strings.TrimSpace(in.Content), EmbeddingModel: l.svcCtx.RAGService.Config().EmbeddingModel},
		})
		if ragErr != nil {
			l.Errorw("rag prepare failed, skip rag", logx.Field("component", "rag"), logx.Field("stage", "prepare"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", ragErr))
		} else if ragResult != nil {
			ragContext = ragResult.ContextMessages
			ragSources = ragResult.Sources
			l.Infow("rag 检索结果",
				logx.Field("query", in.Content),
				logx.Field("rag_result", ragResult),
				logx.Field("conversation_id", prepared.ConversationID),
				logx.Field("user_id", in.UserId))
		}
	}
	_, err = l.runSupervisor(in, prepared, []domain.ContextMessage{
		{Role: domain.ContextRoleUser, Content: strings.TrimSpace(in.Content)},
	}, stream, ragContext, ragSources)
	if err != nil {
		l.Errorw("ai supervisor run failed", logx.Field("component", "ai_chat_logic"), logx.Field("stage", "supervisor_run"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", err))
		return err
	}
	go l.updateConversationMemory(prepared, uint64(in.UserId))
	return nil
}

// 更新记忆
func (l *ChatLogic) updateConversationMemory(prepared *conversation.PreparedConversation, userID uint64) {
	if l.svcCtx == nil || l.svcCtx.SummaryManager == nil || l.svcCtx.MemoryUpdatePublisher == nil || prepared == nil {
		return
	}
	ctx, cancel := context.WithTimeout(contextWithoutCancel(l.ctx), 2*time.Minute)
	defer cancel()

	result, err := l.svcCtx.SummaryManager.MaybeRefresh(ctx, contextmanager.SummaryRefreshRequest{
		UserID:         userID,
		ConversationID: prepared.ConversationID,
	})
	if err != nil {
		l.Errorw("refresh ai conversation summary failed",
			logx.Field("component", "context_manager"),
			logx.Field("stage", "summary_refresh"),
			logx.Field("conversation_id", prepared.ConversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err))
		return
	}
	if !result.Created {
		return
	}
	event := memoryupdate.UpdateEvent{
		EventID:        "memory_evt_" + uuid.NewString(),
		UserID:         userID,
		ConversationID: prepared.ConversationID,
		MessageIDs:     result.CompressedMessageIDs,
		CreatedAt:      time.Now(),
	}
	if err := l.svcCtx.MemoryUpdatePublisher.PublishMemoryUpdate(ctx, event); err != nil {
		l.Errorw("publish ai memory update failed",
			logx.Field("component", "memory_update"),
			logx.Field("stage", "publish_update_event"),
			logx.Field("conversation_id", prepared.ConversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err))
	}
}

func contextWithoutCancel(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
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
			DataJson:       row.Metadata.String,
			Done:           true,
		}); err != nil {
			return err
		}
	}
	return nil
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

func (l *ChatLogic) runSupervisor(in *aiagent.ChatRequest, prepared *conversation.PreparedConversation, agentMessages []domain.ContextMessage, stream agentEventSender, ragContext []domain.ContextMessage, ragSources []domain.AgentSource) ([]*aimessages.AiMessages, error) {
	startedAt := time.Now()
	persistedMessages := make([]*aimessages.AiMessages, 0, 2)
	eventStream, err := l.svcCtx.AgentRunner.Stream(l.ctx, eino.RunRequest{
		UserID:           uint64(in.UserId),
		ConversationID:   prepared.ConversationID,
		MessageID:        newChatMessageID(),
		ClientIP:         clientIPFromContext(l.ctx),
		AccessToken:      accessTokenFromContext(l.ctx),
		RefreshToken:     refreshTokenFromContext(l.ctx),
		RAGContext:       ragContext,
		RAGSources:       ragSources,
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
		l.Infow("事件流处理",
			logx.Field("conversation_id", prepared.ConversationID),
			logx.Field("user_id", in.UserId),
			logx.Field("event", event),
		)
		// 处理需要持久化的事件
		if shouldPersistAgentEvent(event.Type) {
			message, err := agentEventToMessage(uint64(in.UserId), prepared.ClientMessageID, event)
			if err != nil {
				l.Errorw("ai supervisor event persist failed", logx.Field("component", "ai_chat_logic"), logx.Field("stage", "persist_event"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("event_type", event.Type), logx.Field("err", err))
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
			l.Errorw("ai supervisor event send failed", logx.Field("component", "ai_chat_logic"), logx.Field("stage", "supervisor_stream"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("event_type", event.Type), logx.Field("err", err))
			return persistedMessages, err
		}
	}
	l.Infow("事件流处理结束",
		logx.Field("conversation_id", prepared.ConversationID),
		logx.Field("user_id", in.UserId),
		logx.Field("event_count", events),
		logx.Field("business_executed", businessExecuted),
		logx.Field("latency_ms", time.Since(startedAt).Milliseconds()),
	)
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
	} else if event.Type == domain.EventAssistantMessage && strings.TrimSpace(event.DataJSON) != "" {
		metadata = sql.NullString{String: event.DataJSON, Valid: true}
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

func accessTokenFromContext(ctx context.Context) string {
	if values := metadata.ValueFromIncomingContext(ctx, "x-access-token"); len(values) > 0 {
		return values[0]
	}
	return ""
}

func refreshTokenFromContext(ctx context.Context) string {
	if values := metadata.ValueFromIncomingContext(ctx, "x-refresh-token"); len(values) > 0 {
		return values[0]
	}
	return ""
}
