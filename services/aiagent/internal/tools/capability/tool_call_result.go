package capability_tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	toolprompts "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/tools"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

type getToolCallResultParams struct {
	ToolCallID     string `json:"tool_call_id"`
	UserID         uint64 `json:"user_id"`
	ConversationID string `json:"conversation_id"`
}

type getToolCallResultResponse struct {
	ToolCallID   string `json:"tool_call_id"`
	ToolName     string `json:"tool_name"`
	Arguments    string `json:"arguments"`
	Status       string `json:"status"`
	Result       string `json:"result"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func newGetToolCallResultTool(store *contextmanager.ToolCallStore) core.Tool {
	return core.Tool{
		Name:       domain.ToolGetToolCallResult,
		Desc:       toolprompts.GetToolCallResultDesc,
		Params:     toolprompts.GetToolCallResultParameters,
		Kind:       core.ToolKindCapability,
		Visibility: core.ToolVisibilityAllAgents,
		Metadata: domain.Metadata{
			Name:           domain.ToolGetToolCallResult,
			Risk:           domain.RiskLow,
			TimeoutSeconds: 3,
		},
		Handler: getToolCallResultHandler(store),
	}
}

func getToolCallResultHandler(store *contextmanager.ToolCallStore) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		var params getToolCallResultParams
		params.ToolCallID, _ = req.Arguments["tool_call_id"].(string)
		toolCallID := strings.TrimSpace(params.ToolCallID)
		if toolCallID == "" {
			return core.HandlerResult{}, fmt.Errorf("invalid ai tool arguments: tool_call_id is required")
		}
		if strings.TrimSpace(req.ConversationID) == "" {
			return core.HandlerResult{}, errToolHandlerRequired
		}
		if store == nil {
			return core.HandlerResult{}, contextmanager.ErrToolResultNotFound
		}
		row, err := store.FindToolCallByCallID(ctx, req.UserID, req.ConversationID, toolCallID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		result := toolCallResultResponse(row)
		return core.HandlerResult{Data: result, Summary: "已读取历史工具调用结果。"}, nil
	}
}

func toolCallResultResponse(row *aitoolcalls.AiToolCalls) getToolCallResultResponse {
	if row == nil {
		return getToolCallResultResponse{}
	}
	createdAt := ""
	if !row.CreatedAt.IsZero() {
		createdAt = row.CreatedAt.UTC().Format(time.RFC3339)
	}
	return getToolCallResultResponse{
		ToolCallID:   row.ToolCallId,
		ToolName:     row.ToolName,
		Arguments:    row.Arguments,
		Status:       row.Status,
		Result:       row.Result,
		ErrorMessage: row.ErrorMessage,
		CreatedAt:    createdAt,
	}
}
