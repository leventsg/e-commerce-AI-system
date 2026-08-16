package eino

import (
	"context"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

type toolCallContextMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newToolCallContextMiddleware() adk.ChatModelAgentMiddleware {
	return &toolCallContextMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

// 包装可调用工具调用端点，在调用前将tool_call_id注入到上下文中
func (m *toolCallContextMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if tCtx == nil || tCtx.CallID == "" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
		return endpoint(withToolCallID(ctx, tCtx.CallID), argumentsInJSON, opts...)
	}, nil
}

// 将tool_call_id注入到工具执行上下文中
func withToolCallID(ctx context.Context, toolCallID string) context.Context {
	execution, ok := helper.ToolExecutionFromContext(ctx)
	if !ok {
		return ctx
	}
	execution.ToolCallID = toolCallID
	return helper.WithToolExecutionContext(ctx, execution)
}
