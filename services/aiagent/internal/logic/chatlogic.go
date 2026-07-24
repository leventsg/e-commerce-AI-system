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
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"

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
	if l.svcCtx == nil || l.svcCtx.ConversationManager == nil || l.svcCtx.ContextManager == nil || l.svcCtx.IntentPlanner == nil || l.svcCtx.MessagesModel == nil {
		return chatErrorResponse(in.ConversationId, errors.New("AI 服务暂时不可用，请稍后重试")), nil
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "web"
	}
	metadata, _ := json.Marshal(map[string]string{"source": source})
	// 会话初始化
	prepared, err := l.svcCtx.ConversationManager.Prepare(l.ctx, conversation.PrepareRequest{
		UserID: uint64(in.UserId), ConversationID: in.ConversationId, MessageID: in.MessageId,
		Content: strings.TrimSpace(in.Content), Metadata: sql.NullString{String: string(metadata), Valid: true},
	})
	if err != nil || prepared == nil {
		if err == nil {
			err = errors.New("会话初始化失败")
		}
		return chatErrorResponse(in.ConversationId, err), nil
	}
	currentInput := strings.TrimSpace(in.Content)
	intentContext, err := l.svcCtx.ContextManager.Build(l.ctx, domain.BuildContextRequest{
		UserID:           uint64(in.UserId),
		ConversationID:   prepared.ConversationID,
		Mode:             domain.IntentContextMode,
		CurrentMessageID: prepared.UserMessageID,
		CurrentInput:     currentInput,
	})
	if err != nil || intentContext == nil {
		if err == nil {
			err = errors.New("意图上下文构建失败")
		}
		return chatErrorResponse(prepared.ConversationID, err), nil
	}
	// 意图识别
	plan, err := l.svcCtx.IntentPlanner.Plan(l.ctx, planner.PlanRequest{Message: currentInput, Messages: intentContext.Messages})
	if err != nil {
		return chatErrorResponse(prepared.ConversationID, err), nil
	}

	var agentMessages []domain.ContextMessage
	if planUsesAgentRunner(plan) {
		agentContext, buildErr := l.svcCtx.ContextManager.Build(l.ctx, domain.BuildContextRequest{
			UserID:           uint64(in.UserId),
			ConversationID:   prepared.ConversationID,
			Mode:             domain.AgentContextMode,
			CurrentMessageID: prepared.UserMessageID,
			CurrentInput:     currentInput,
		})
		if buildErr != nil || agentContext == nil {
			if buildErr == nil {
				buildErr = errors.New("对话上下文构建失败")
			}
			return chatErrorResponse(prepared.ConversationID, buildErr), nil
		}
		agentMessages = agentContext.Messages
	}

	// 根据意图执行相应的操作，并生成事件
	events := l.executePlan(in, prepared, plan, agentMessages)
	protoEvents := make([]*aiagent.AgentEvent, 0, len(events))
	businessExecuted := false
	for i := range events {
		if strings.TrimSpace(events[i].ConversationID) == "" {
			events[i].ConversationID = prepared.ConversationID
		}
		if strings.TrimSpace(events[i].MessageID) == "" || events[i].MessageID == prepared.UserMessageID {
			events[i].MessageID = newChatMessageID()
		}
		if events[i].BusinessExecuted {
			businessExecuted = true
		}
		protoEvents = append(protoEvents, agentEventToProto(events[i]))
	}
	// 将事件转换为数据库消息记录格式
	messages, err := agentEventsToMessages(uint64(in.UserId), events)
	if err != nil {
		protoEvents = append(protoEvents, persistenceErrorEvent(prepared.ConversationID, businessExecuted))
		return &aiagent.ChatResponse{StatusCode: code.ServerError, StatusMsg: err.Error(), Events: protoEvents}, nil
	}
	// 批量插入消息记录
	if err := l.svcCtx.MessagesModel.InsertBatch(l.ctx, messages); err != nil {
		protoEvents = append(protoEvents, persistenceErrorEvent(prepared.ConversationID, businessExecuted))
		return &aiagent.ChatResponse{StatusCode: code.ServerError, StatusMsg: err.Error(), Events: protoEvents}, nil
	}
	l.refreshConversationSummary(prepared.ConversationID, uint64(in.UserId))
	return &aiagent.ChatResponse{StatusCode: code.Success, StatusMsg: code.SuccessMsg, Events: protoEvents}, nil
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

// executePlan 根据意图执行相应的操作，并生成事件
func (l *ChatLogic) executePlan(in *aiagent.ChatRequest, prepared *conversation.PreparedConversation, plan planner.PlanResult, agentMessages []domain.ContextMessage) []domain.AgentEvent {
	if len(plan.MissingParams) > 0 || (plan.Intent != planner.IntentChat && strings.TrimSpace(plan.AssistantMessage) != "") {
		return []domain.AgentEvent{{Type: domain.EventAssistantMessage, ConversationID: prepared.ConversationID, Content: strings.TrimSpace(plan.AssistantMessage), Done: true}}
	}
	if strings.TrimSpace(plan.ToolName) == "" {
		if l.svcCtx.AgentRunner == nil {
			l.Errorw("ai chat runner unavailable", logx.Field("component", "chat_model"), logx.Field("stage", "execute"), logx.Field("reason", "runner_unavailable"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId))
			return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "AI 服务暂时不可用，请稍后重试", Done: true}}
		}
		events, err := l.svcCtx.AgentRunner.Run(l.ctx, eino.RunRequest{
			ConversationID: prepared.ConversationID,
			MessageID:      newChatMessageID(),
			Messages:       agentMessages,
		})
		if err != nil {
			l.Errorw("ai chat model execution failed", logx.Field("component", "chat_model"), logx.Field("stage", "execute"), logx.Field("reason", eino.ErrorReason(err)), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId), logx.Field("err", err))
			return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "AI 服务暂时不可用，请稍后重试", Done: true}}
		}
		if len(events) == 0 {
			l.Errorw("ai chat model returned no events", logx.Field("component", "chat_model"), logx.Field("stage", "execute"), logx.Field("reason", "model_empty_response"), logx.Field("conversation_id", prepared.ConversationID), logx.Field("user_id", in.UserId))
			return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "AI 服务暂时不可用，请稍后重试", Done: true}}
		}
		return events
	}
	args := make(map[string]any, len(plan.Arguments))
	for key, value := range plan.Arguments {
		if !strings.EqualFold(key, "user_id") {
			args[key] = value
		}
	}
	req := tools.ExecuteRequest{UserID: uint64(in.UserId), ConversationID: prepared.ConversationID, MessageID: newChatMessageID(), ClientIP: clientIPFromContext(l.ctx), ToolName: plan.ToolName, Arguments: args}
	requiresConfirmation := plan.RequireConfirmation
	if l.svcCtx.ToolRegistry != nil {
		if metadata, err := l.svcCtx.ToolRegistry.Metadata(plan.ToolName); err == nil && metadata.RequireConfirmation {
			requiresConfirmation = true
		}
	}
	var event domain.AgentEvent
	switch {
	// 如果需要用户确认，则生成确认请求事件
	case requiresConfirmation:
		if l.svcCtx.HighRiskChatTools == nil {
			return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "确认服务暂时不可用", Done: true}}
		}
		event = l.svcCtx.HighRiskChatTools.RequestConfirmation(l.ctx, req)
	// 如果是查询或推荐意图，则执行查询工具
	case plan.Intent == planner.IntentQuery || plan.Intent == planner.IntentRecommend:
		if l.svcCtx.QueryChatTools == nil {
			return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "查询服务暂时不可用", Done: true}}
		}
		event = l.svcCtx.QueryChatTools.Execute(l.ctx, req)
	default:
		// 执行写入工具，例如添加商品到购物车、减少购物车商品等操作
		if l.svcCtx.WriteChatTools == nil {
			return []domain.AgentEvent{{Type: domain.EventError, ConversationID: prepared.ConversationID, Content: "操作服务暂时不可用", Done: true}}
		}
		event = l.svcCtx.WriteChatTools.Execute(l.ctx, req)
	}
	events := []domain.AgentEvent{event}
	if event.Type == domain.EventToolResult && strings.TrimSpace(event.Content) != "" {
		events = append(events, domain.AgentEvent{Type: domain.EventAssistantMessage, ConversationID: prepared.ConversationID, MessageID: newChatMessageID(), Content: event.Content, Done: true})
	}
	return events
}

func planUsesAgentRunner(plan planner.PlanResult) bool {
	return plan.Intent == planner.IntentChat &&
		len(plan.MissingParams) == 0 &&
		strings.TrimSpace(plan.ToolName) == ""
}

// agentEventsToMessages 将事件转换为数据库消息记录格式
func agentEventsToMessages(userID uint64, events []domain.AgentEvent) ([]*aimessages.AiMessages, error) {
	messages := make([]*aimessages.AiMessages, 0, len(events))
	for _, event := range events {
		message, err := agentEventToMessage(userID, event)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// agentEventToMessage 将事件转换为数据库消息记录格式
func agentEventToMessage(userID uint64, event domain.AgentEvent) (*aimessages.AiMessages, error) {
	role := conversation.RoleAssistant
	metadata := sql.NullString{}
	if event.Type == domain.EventToolResult || event.Type == domain.EventConfirmationRequired {
		role = conversation.RoleTool
		raw, err := contextmanager.BuildToolResultMetadata(event.MessageID, event.Tool, event.Status, event.ConfirmationID, event.DataJSON, event.Content)
		if err != nil {
			return nil, err
		}
		metadata = sql.NullString{String: raw, Valid: true}
	}
	return &aimessages.AiMessages{Id: event.MessageID, ConversationId: event.ConversationID, UserId: userID, Role: role, Content: event.Content, Metadata: metadata, CreatedAt: time.Now()}, nil
}

func (l *ChatLogic) validateRequest(in *aiagent.ChatRequest) error {
	if in == nil || in.UserId == 0 {
		return errors.New("用户身份无效")
	}
	if strings.TrimSpace(in.Content) == "" {
		return errors.New("消息内容不能为空")
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

func newChatMessageID() string { return "msg_" + uuid.NewString() }

func clientIPFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(biz.ClientIPKey).(string); ok {
		return value
	}
	if values := metadata.ValueFromIncomingContext(ctx, "x-client-ip"); len(values) > 0 {
		return values[0]
	}
	return ""
}
