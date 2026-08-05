package logic

import (
	"context"
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
	// 决定是否执行高风险操作
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
	if strings.TrimSpace(decided.CheckpointID) == "" || strings.TrimSpace(decided.InterruptID) == "" {
		if in.Approved {
			_, _ = l.svcCtx.ConfirmationManager.MarkFailed(l.ctx, confirmation.CompletionRequest{
				UserID:         uint64(in.UserId),
				ConversationID: decided.ConversationID,
				ConfirmationID: decided.ID,
			})
		}
		return sendConfirmError(stream, decided.ConversationID, fmt.Errorf("确认恢复点不存在，请重新发起操作"))
	}

	businessExecuted := false
	markExecuted := false
	domainEvents := make([]domain.AgentEvent, 0, 2)
	_, err = l.svcCtx.AgentRunner.Resume(l.ctx, eino.ResumeRequest{
		UserID:         uint64(in.UserId),
		ConversationID: decided.ConversationID,
		ConfirmationID: decided.ID,
		RunID:          decided.RunID,
		CheckpointID:   decided.CheckpointID,
		InterruptID:    decided.InterruptID,
		Approved:       in.Approved,
		OnEvent: func(ctx context.Context, event domain.AgentEvent) error {
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
				if _, markErr := l.svcCtx.ConfirmationManager.MarkExecuted(ctx, confirmation.CompletionRequest{
					UserID:         uint64(in.UserId),
					ConversationID: decided.ConversationID,
					ConfirmationID: decided.ID,
				}); markErr != nil {
					l.Errorw("mark confirmed tool executed failed", logx.Field("confirmation_id", decided.ID), logx.Field("err", markErr))
					_ = stream.Send(completionErrorEvent(decided.ConversationID, "业务操作已完成，但确认状态保存失败", markErr))
				}
				markExecuted = true
			}
			messages, msgErr := agentEventsToMessages(uint64(in.UserId), "", []domain.AgentEvent{event})
			if msgErr != nil {
				return msgErr
			}
			if l.svcCtx.MessagesModel != nil {
				if msgErr = l.svcCtx.MessagesModel.InsertBatch(ctx, messages); msgErr != nil {
					_ = stream.Send(persistenceErrorEvent(event.ConversationID, businessExecuted))
					return msgErr
				}
			}
			domainEvents = append(domainEvents, event)
			return stream.Send(agentEventToProto(event))
		},
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
	if l.svcCtx == nil || l.svcCtx.ConfirmationManager == nil || l.svcCtx.AgentRunner == nil {
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
