package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ConfirmActionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewConfirmActionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConfirmActionLogic {
	return &ConfirmActionLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ConfirmAction 处理用户对高风险操作的确认请求，并流式返回恢复事件。
func (l *ConfirmActionLogic) ConfirmAction(in *aiagent.ConfirmActionRequest, stream agentEventSender) error {
	if stream == nil {
		return fmt.Errorf("stream 为空")
	}
	if err := l.validateRequest(in); err != nil {
		return sendConfirmError(stream, "", err)
	}
	// 更新数据库
	decided, err := l.svcCtx.ConfirmationManager.Decide(l.ctx, confirmation.DecisionRequest{
		UserID:         uint64(in.UserId),
		ConversationID: strings.TrimSpace(in.ConversationId),
		ConfirmationID: strings.TrimSpace(in.ConfirmationId),
		Approved:       in.Approved,
	})
	if err != nil {
		return sendConfirmError(stream, in.ConversationId, err)
	}
	if decided == nil {
		return sendConfirmError(stream, in.ConversationId, fmt.Errorf("确认记录为空"))
	}
	if !in.Approved {
		return l.rejectConfirmation(decided, uint64(in.UserId), stream)
	}
	if l.svcCtx.AgentRunner == nil {
		return sendConfirmError(stream, decided.ConversationID, fmt.Errorf("确认服务暂不可用"))
	}
	if decided.CheckpointID == "" || decided.InterruptID == "" {
		_, _ = l.svcCtx.ConfirmationManager.MarkFailed(l.ctx, confirmation.CompletionRequest{
			UserID:         uint64(in.UserId),
			ConversationID: decided.ConversationID,
			ConfirmationID: decided.ID,
		})
		return sendConfirmError(stream, decided.ConversationID, fmt.Errorf("确认恢复点不存在，请重新发起操作"))
	}

	businessExecuted := false
	markExecuted := false
	domainEvents := make([]domain.AgentEvent, 0, 2)
	eventStream, err := l.svcCtx.AgentRunner.ResumeStream(l.ctx, eino.ResumeRequest{
		UserID:         uint64(in.UserId),
		ConversationID: decided.ConversationID,
		ConfirmationID: decided.ID,
		RunID:          decided.RunID,
		CheckpointID:   decided.CheckpointID,
		InterruptID:    decided.InterruptID,
		Approved:       in.Approved,
	})
	if err != nil {
		if in.Approved {
			_, _ = l.svcCtx.ConfirmationManager.MarkFailed(l.ctx, confirmation.CompletionRequest{
				UserID:         uint64(in.UserId),
				ConversationID: decided.ConversationID,
				ConfirmationID: decided.ID,
			})
		}
		return sendConfirmError(stream, decided.ConversationID, err)
	}
	forwardState := newEventForwardState()
	for event := range eventStream {
		if strings.TrimSpace(event.MessageID) == "" {
			event.MessageID = newChatMessageID()
		}
		if strings.TrimSpace(event.ConversationID) == "" {
			event.ConversationID = decided.ConversationID
		}
		if event.BusinessExecuted || (event.Type == domain.EventToolResult && event.Status == "success" && event.Tool == decided.ToolName) {
			businessExecuted = true
		}
		if in.Approved && businessExecuted && !markExecuted {
			if _, markErr := l.svcCtx.ConfirmationManager.MarkExecuted(l.ctx, confirmation.CompletionRequest{
				UserID:         uint64(in.UserId),
				ConversationID: decided.ConversationID,
				ConfirmationID: decided.ID,
			}); markErr != nil {
				l.Errorw("mark confirmed tool executed failed", logx.Field("confirmation_id", decided.ID), logx.Field("err", markErr))
				_ = stream.Send(completionErrorEvent(decided.ConversationID, "业务操作已完成，但确认状态保存失败", markErr))
			}
			markExecuted = true
		}
		forward := forwardState.shouldForward(event)
		if isTransientAgentEvent(event.Type) {
			if !forward {
				continue
			}
			if err := stream.Send(agentEventToProto(event)); err != nil {
				return err
			}
			domainEvents = append(domainEvents, event)
			continue
		}
		messages, msgErr := agentEventsToMessages(uint64(in.UserId), "", []domain.AgentEvent{event})
		if msgErr != nil {
			return msgErr
		}
		if l.svcCtx.MessagesModel != nil {
			if msgErr = l.svcCtx.MessagesModel.InsertBatch(l.ctx, messages); msgErr != nil {
				_ = stream.Send(persistenceErrorEvent(event.ConversationID, businessExecuted))
				return msgErr
			}
		}
		domainEvents = append(domainEvents, event)
		if !forward {
			continue
		}
		if err := stream.Send(agentEventToProto(event)); err != nil {
			return err
		}
	}
	if in.Approved && !businessExecuted {
		if _, markErr := l.svcCtx.ConfirmationManager.MarkFailed(l.ctx, confirmation.CompletionRequest{
			UserID:         uint64(in.UserId),
			ConversationID: decided.ConversationID,
			ConfirmationID: decided.ID,
		}); markErr != nil {
			l.Errorw("mark confirmed tool failed failed", logx.Field("confirmation_id", decided.ID), logx.Field("err", markErr))
			return stream.Send(completionErrorEvent(decided.ConversationID, "业务操作失败，且确认失败状态保存失败", markErr))
		}
	}
	if len(domainEvents) == 0 {
		return sendConfirmError(stream, decided.ConversationID, fmt.Errorf("确认服务未返回有效事件"))
	}
	return nil
}

// rejectConfirmation 拒绝确认请求
func (l *ConfirmActionLogic) rejectConfirmation(decided *domain.Confirmation, userID uint64, stream agentEventSender) error {
	events := rejectedConfirmationEvents(decided)
	messages, err := agentEventsToMessages(userID, "", events)
	if err != nil {
		return err
	}
	if l.svcCtx.MessagesModel != nil {
		if err := l.svcCtx.MessagesModel.InsertBatch(l.ctx, messages); err != nil {
			_ = stream.Send(persistenceErrorEvent(decided.ConversationID, false))
			return err
		}
	}
	for _, event := range events {
		if err := stream.Send(agentEventToProto(event)); err != nil {
			return err
		}
	}
	return nil
}

// 组装tool_result事件和assistant消息事件，返回给前端，且用于数据库保存
func rejectedConfirmationEvents(decided *domain.Confirmation) []domain.AgentEvent {
	conversationID := decided.ConversationID
	toolName := decided.ToolName
	summary := "操作已取消。"
	raw, _ := json.Marshal(map[string]any{
		"tool_name": toolName,
		"status":    confirmation.StatusRejected,
		"summary":   summary,
	})
	assistantContent := rejectedAssistantContent(toolName)
	return []domain.AgentEvent{
		{
			Type:           domain.EventToolResult,
			ConversationID: conversationID,
			MessageID:      newChatMessageID(),
			Tool:           toolName,
			Status:         confirmation.StatusRejected,
			Content:        summary,
			DataJSON:       string(raw),
			Done:           true,
		},
		{
			Type:           domain.EventAssistantMessage,
			ConversationID: conversationID,
			MessageID:      newChatMessageID(),
			Content:        assistantContent,
			Done:           true,
		},
	}
}

func rejectedAssistantContent(toolName string) string {
	switch toolName {
	case domain.ToolCartDelete:
		return "已取消删除操作，购物车商品不会被移除。"
	case domain.ToolOrderCreate:
		return "已取消下单操作，不会创建订单。"
	case domain.ToolOrderCancel:
		return "已取消取消订单操作，订单状态不会被修改。"
	default:
		return "已取消该操作，相关内容不会被修改。"
	}
}

func completionErrorEvent(conversationID, message string, err error) *aiagent.AgentEvent {
	return &aiagent.AgentEvent{
		Type:           domain.EventError,
		ConversationId: conversationID,
		Content:        fmt.Sprintf("%s：%v", message, err),
		Status:         "failed",
		Done:           true,
	}
}

func (l *ConfirmActionLogic) validateRequest(in *aiagent.ConfirmActionRequest) error {
	if in == nil || in.UserId == 0 || strings.TrimSpace(in.ConversationId) == "" || strings.TrimSpace(in.ConfirmationId) == "" {
		return fmt.Errorf("确认请求参数不完整")
	}
	if l.svcCtx == nil || l.svcCtx.ConfirmationManager == nil {
		return fmt.Errorf("确认服务暂不可用")
	}
	return nil
}

func sendConfirmError(stream agentEventSender, conversationID string, err error) error {
	message := "确认服务暂时不可用，请稍后重试"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return stream.Send(&aiagent.AgentEvent{Type: domain.EventError, ConversationId: conversationID, MessageId: newChatMessageID(), Content: message, Done: true})
}

// agentEventToProto 将 domain.AgentEvent 转换为 aiagent.AgentEvent
func agentEventToProto(event domain.AgentEvent) *aiagent.AgentEvent {
	return &aiagent.AgentEvent{
		Type:           event.Type,
		ConversationId: event.ConversationID,
		MessageId:      event.MessageID,
		Content:        event.Content,
		Tool:           event.Tool,
		Status:         event.Status,
		DataJson:       event.DataJSON,
		ConfirmationId: event.ConfirmationID,
		Action:         event.Action,
		Summary:        event.Summary,
		ExpiresAt:      event.ExpiresAt,
		Done:           event.Done,
	}
}
