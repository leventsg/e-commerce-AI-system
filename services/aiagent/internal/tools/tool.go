package tools

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type ConfirmationSummaryFunc func(context.Context, ExecuteRequest) (string, error)

type Tool struct {
	Name                string
	Desc                string
	Params              map[string]*schema.ParameterInfo
	Metadata            domain.Metadata
	Handler             HandlerFunc
	ConfirmationSummary ConfirmationSummaryFunc
}

type invokableToolAdapter struct {
	tool     Tool
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
	execution, ok := ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 {
		return "", ErrToolExecutionContext
	}
	if t.executor == nil {
		return "", ErrToolHandlerRequired
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		parseErr := fmt.Errorf("%w: invalid JSON arguments: %v", ErrInvalidToolArguments, err)
		event := t.executor.Reject(ctx, executeRequestFromContext(execution, t.tool.Name, nil), parseErr)
		return event.DataJSON, fmt.Errorf("%w: %s", ErrToolExecution, event.Content)
	}
	event := t.executor.Execute(ctx, executeRequestFromContext(execution, t.tool.Name, args), t.tool.Handler)
	if event.Status != toolStatusSuccess {
		return event.DataJSON, fmt.Errorf("%w: %s", ErrToolExecution, event.Content)
	}
	return event.DataJSON, nil
}
