package logic

import (
	"context"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"

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

// ConfirmAction 处理用户对高风险操作的确认请求
func (l *ConfirmActionLogic) ConfirmAction(in *aiagent.ConfirmActionRequest) (*aiagent.ConfirmActionResponse, error) {
	if err := l.validateRequest(in); err != nil {
		return confirmErrorResponse(err), nil
	}
	// 决定是否执行高风险操作
	decided, err := l.svcCtx.ConfirmationManager.Decide(l.ctx, confirmation.DecisionRequest{
		UserID:         uint64(in.UserId),
		ConversationID: strings.TrimSpace(in.ConversationId),
		ConfirmationID: strings.TrimSpace(in.ConfirmationId),
		Approved:       in.Approved,
	})
	if err != nil {
		return confirmErrorResponse(err), nil
	}
	if decided == nil {
		return confirmErrorResponse(fmt.Errorf("确认记录为空")), nil
	}
	// 如果用户拒绝了确认请求，则不执行高风险操作，只返回一个取消的事件
	if !in.Approved {
		resp := &aiagent.ConfirmActionResponse{
			StatusMsg: "操作已取消",
			Events: []*aiagent.AgentEvent{{
				Type:           domain.EventAssistantMessage,
				ConversationId: decided.ConversationID,
				Content:        "操作已取消。",
				Done:           true,
			}},
		}
		l.persistConfirmationEvents(uint64(in.UserId), false, resp)
		return resp, nil
	}

	// 如果用户批准了确认请求，则执行高风险操作，并根据执行结果更新确认状态
	event := l.svcCtx.HighRiskTools.ExecuteConfirmed(l.ctx, tools.ExecuteRequest{
		UserID:         uint64(in.UserId),
		ConversationID: decided.ConversationID,
		ToolName:       decided.ToolName,
		Arguments:      decided.Arguments,
	})
	completion := confirmation.CompletionRequest{
		UserID:         uint64(in.UserId),
		ConversationID: decided.ConversationID,
		ConfirmationID: decided.ID,
	}
	events := []*aiagent.AgentEvent{agentEventToProto(event)}
	if event.Status == "success" || event.BusinessExecuted {
		if _, markErr := l.svcCtx.ConfirmationManager.MarkExecuted(l.ctx, completion); markErr != nil {
			l.Errorw("mark confirmed tool executed failed", logx.Field("confirmation_id", decided.ID), logx.Field("err", markErr))
			events = append(events, completionErrorEvent(decided.ConversationID, "业务操作已完成，但确认状态保存失败", markErr))
		}
	} else {
		if _, markErr := l.svcCtx.ConfirmationManager.MarkFailed(l.ctx, completion); markErr != nil {
			l.Errorw("mark confirmed tool failed failed", logx.Field("confirmation_id", decided.ID), logx.Field("err", markErr))
			events = append(events, completionErrorEvent(decided.ConversationID, "业务操作失败，且确认失败状态保存失败", markErr))
		}
	}
	resp := &aiagent.ConfirmActionResponse{
		StatusMsg: event.Content,
		Events:    events,
	}
	// 将确认操作的结果事件保存到数据库中
	l.persistConfirmationEvents(uint64(in.UserId), event.Status == "success" || event.BusinessExecuted, resp)
	return resp, nil
}

// persistConfirmationEvents 将确认操作的结果事件保存到数据库中
func (l *ConfirmActionLogic) persistConfirmationEvents(userID uint64, businessExecuted bool, resp *aiagent.ConfirmActionResponse) {
	if l.svcCtx == nil || l.svcCtx.MessagesModel == nil || resp == nil {
		return
	}
	domainEvents := make([]domain.AgentEvent, 0, len(resp.Events))
	for _, event := range resp.Events {
		if event == nil {
			continue
		}
		if strings.TrimSpace(event.MessageId) == "" {
			event.MessageId = newChatMessageID()
		}
		domainEvents = append(domainEvents, domain.AgentEvent{Type: event.Type, ConversationID: event.ConversationId, MessageID: event.MessageId, Content: event.Content, Tool: event.Tool, Status: event.Status, DataJSON: event.DataJson, ConfirmationID: event.ConfirmationId, Action: event.Action, Summary: event.Summary, ExpiresAt: event.ExpiresAt, Done: event.Done, BusinessExecuted: businessExecuted})
	}
	messages, err := agentEventsToMessages(userID, "", domainEvents)
	if err == nil {
		err = l.svcCtx.MessagesModel.InsertBatch(l.ctx, messages)
	}
	if err != nil {
		conversationID := ""
		if len(domainEvents) > 0 {
			conversationID = domainEvents[0].ConversationID
		}
		l.Errorw("persist confirmation events failed", logx.Field("conversation_id", conversationID), logx.Field("err", err))
		resp.StatusCode = 500
		resp.StatusMsg = err.Error()
		resp.Events = append(resp.Events, persistenceErrorEvent(conversationID, businessExecuted))
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
	if l.svcCtx == nil || l.svcCtx.ConfirmationManager == nil || l.svcCtx.HighRiskTools == nil {
		return fmt.Errorf("确认服务暂不可用")
	}
	return nil
}

func confirmErrorResponse(err error) *aiagent.ConfirmActionResponse {
	message := "确认操作失败，请稍后重试。"
	if err != nil {
		message = err.Error()
	}
	return &aiagent.ConfirmActionResponse{
		StatusCode: 1,
		StatusMsg:  message,
		Events: []*aiagent.AgentEvent{{
			Type:    domain.EventError,
			Content: message,
			Status:  "failed",
			Done:    true,
		}},
	}
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
