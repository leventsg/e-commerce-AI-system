package tools

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

type invokableToolAdapter struct {
	tool     core.Tool
	executor *Executor
}

func (t *invokableToolAdapter) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.tool.Name,
		Desc:        t.tool.Desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(t.tool.Params),
	}, nil
}

func (t *invokableToolAdapter) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	execution, ok := helper.ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 {
		return "", helper.ErrToolExecutionContext
	}
	if t.executor == nil {
		return "", ErrToolHandlerRequired
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		parseErr := fmt.Errorf("%w: invalid JSON arguments: %v", helper.ErrInvalidToolArguments, err)
		event := t.executor.Reject(ctx, helper.ExecuteRequestFromContext(execution, t.tool.Name, nil), parseErr)
		return event.DataJSON, nil
	}
	event := t.executor.Execute(ctx, helper.ExecuteRequestFromContext(execution, t.tool.Name, args), t.tool.Handler)
	if event.Status != toolStatusSuccess {
		return event.DataJSON, nil
	}
	return event.DataJSON, nil
}
