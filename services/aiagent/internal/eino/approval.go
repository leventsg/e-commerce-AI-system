package eino

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

type ApprovalInfo struct {
	ConfirmationID   string         `json:"confirmation_id"`
	ToolName         string         `json:"tool_name"`
	Action           string         `json:"action"`
	Summary          string         `json:"summary"`
	ExpiresAt        int64          `json:"expires_at"`
	ArgumentsSummary map[string]any `json:"arguments_summary,omitempty"`
}

type ApprovalResult struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

type approvalRunMeta struct {
	RunID        string
	CheckpointID string
}

type approvalRunMetaKey struct{}

type highRiskApprovalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	approvalManager *aitools.ApprovalManager
}

func newHighRiskApprovalMiddleware(approvalManager *aitools.ApprovalManager) adk.ChatModelAgentMiddleware {
	return &highRiskApprovalMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		approvalManager:              approvalManager,
	}
}

// WrapInvokableToolCall 在真正执行工具之前调用，判断是否需要中断
func (m *highRiskApprovalMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if m == nil || m.approvalManager == nil || tCtx == nil || !m.approvalManager.RequiresConfirmation(tCtx.Name) {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
		// 检查是否已经中断过
		wasInterrupted, _, storedArgs := einotool.GetInterruptState[string](ctx)
		if !wasInterrupted {
			// 首次调用，创建确认请求并中断执行
			info, err := m.createApprovalInfo(ctx, tCtx.Name, argumentsInJSON)
			if err != nil {
				return "", err
			}
			return "", einotool.StatefulInterrupt(ctx, info, argumentsInJSON)
		}

		// 已经中断过，检查是否有中断恢复结果
		isTarget, hasData, data := einotool.GetResumeContext[*ApprovalResult](ctx)
		if isTarget && hasData {
			if data != nil && data.Approved {
				return endpoint(ctx, storedArgs, opts...)
			}
			return marshalApprovalToolResult(tCtx.Name, "rejected", "操作已取消。"), nil
		}

		isTarget, _, _ = einotool.GetResumeContext[any](ctx)
		if !isTarget {
			info, err := m.createApprovalInfo(ctx, tCtx.Name, storedArgs)
			if err != nil {
				return "", err
			}
			return "", einotool.StatefulInterrupt(ctx, info, storedArgs)
		}

		return endpoint(ctx, storedArgs, opts...)
	}, nil
}

// createApprovalInfo 创建确认请求并返回确认信息
func (m *highRiskApprovalMiddleware) createApprovalInfo(ctx context.Context, toolName, argumentsInJSON string) (*ApprovalInfo, error) {
	execution, ok := aitools.ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 {
		return nil, aitools.ErrToolExecutionContext
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON arguments: %v", aitools.ErrInvalidToolArguments, err)
	}
	event := m.approvalManager.RequestConfirmation(ctx, aitools.ExecuteRequest{
		UserID:         execution.UserID,
		ConversationID: execution.ConversationID,
		MessageID:      execution.MessageID,
		ClientIP:       execution.ClientIP,
		RunID:          execution.RunID,
		CheckpointID:   execution.CheckpointID,
		ToolName:       toolName,
		Arguments:      args,
	})
	if event.Type != domain.EventConfirmationRequired {
		return nil, fmt.Errorf("%w: %s", aitools.ErrToolExecution, event.Content)
	}
	return &ApprovalInfo{
		ConfirmationID:   event.ConfirmationID,
		ToolName:         event.Tool,
		Action:           event.Action,
		Summary:          event.Summary,
		ExpiresAt:        event.ExpiresAt,
		ArgumentsSummary: args,
	}, nil
}

func withApprovalRunMeta(ctx context.Context, meta approvalRunMeta) context.Context {
	return context.WithValue(ctx, approvalRunMetaKey{}, meta)
}

func approvalRunMetaFromContext(ctx context.Context) approvalRunMeta {
	meta, _ := ctx.Value(approvalRunMetaKey{}).(approvalRunMeta)
	return meta
}

// 解析中断信息中的 ApprovalInfo
func interruptEventToDomainEvent(ctx context.Context, info *adk.InterruptInfo, req RunRequest, approvalManager *aitools.ApprovalManager) (domain.AgentEvent, bool, error) {
	if info == nil {
		return domain.AgentEvent{}, false, nil
	}
	for _, item := range info.InterruptContexts {
		if item == nil || !item.IsRootCause {
			continue
		}
		approval, ok := item.Info.(*ApprovalInfo)
		if !ok || approval == nil || approval.ConfirmationID == "" {
			continue
		}
		meta := approvalRunMetaFromContext(ctx)
		if approvalManager != nil {
			if err := approvalManager.BindResumeTarget(ctx, confirmation.ResumeTargetRequest{
				UserID:         req.UserID,
				ConversationID: req.ConversationID,
				ConfirmationID: approval.ConfirmationID,
				RunID:          meta.RunID,
				CheckpointID:   meta.CheckpointID,
				InterruptID:    item.ID,
			}); err != nil {
				return domain.AgentEvent{}, false, err
			}
		}
		payload := map[string]any{
			"type":              domain.EventConfirmationRequired,
			"confirmation_id":   approval.ConfirmationID,
			"run_id":            meta.RunID,
			"checkpoint_id":     meta.CheckpointID,
			"interrupt_id":      item.ID,
			"action":            approval.Action,
			"summary":           approval.Summary,
			"expires_at":        approval.ExpiresAt,
			"arguments_summary": approval.ArgumentsSummary,
		}
		raw, _ := json.Marshal(payload)
		return domain.AgentEvent{
			Type:           domain.EventConfirmationRequired,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			Content:        approval.Summary,
			Tool:           approval.ToolName,
			Status:         confirmation.StatusPending,
			DataJSON:       string(raw),
			ConfirmationID: approval.ConfirmationID,
			Action:         approval.Action,
			Summary:        approval.Summary,
			ExpiresAt:      approval.ExpiresAt,
			Done:           true,
		}, true, nil
	}
	return domain.AgentEvent{}, false, nil
}

func marshalApprovalToolResult(toolName, status, summary string) string {
	raw, _ := json.Marshal(map[string]any{
		"tool_name": toolName,
		"status":    status,
		"summary":   summary,
	})
	return string(raw)
}

func init() {
	schema.RegisterName[*ApprovalInfo]("_go_mall_ai_approval_info")
	schema.RegisterName[*ApprovalResult]("_go_mall_ai_approval_result")
}
