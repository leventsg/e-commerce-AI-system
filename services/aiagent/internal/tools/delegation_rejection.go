package tools

import (
	"context"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

// RejectDelegation records a denied delegated business call through the same
// executor/audit path as other validation failures, without invoking its RPC.
func (r *Registry) RejectDelegation(ctx context.Context, name string, args map[string]any, code, message string) (string, error) {
	execution, ok := helper.ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 {
		return "", helper.ErrToolExecutionContext
	}
	if r == nil || r.executor == nil {
		return "", ErrToolHandlerRequired
	}
	cause := permanentToolError(code, message, BusinessOutcomeNotExecuted, nil)
	event := r.executor.Reject(ctx, helper.ExecuteRequestFromContext(execution, name, args), cause)
	return event.DataJSON, nil
}
