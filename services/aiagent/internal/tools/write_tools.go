package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

var ErrWriteToolExecution = errors.New("write tool execution failed")

type WriteToolClients struct {
	Cart   CartWriteRPC
	Coupon CouponWriteRPC
}

type WriteTools struct {
	executor *Executor
	handlers map[string]HandlerFunc
}

func NewWriteTools(executor *Executor, clients WriteToolClients) *WriteTools {
	handlers := make(map[string]HandlerFunc)
	registerQueryHandlers(handlers, cartWriteHandlers(clients.Cart))
	registerQueryHandlers(handlers, couponWriteHandlers(clients.Coupon))
	writeTools := &WriteTools{executor: executor, handlers: handlers}
	writeTools.bindEinoTools()
	return writeTools
}

func (w *WriteTools) Execute(ctx context.Context, req ExecuteRequest) domain.AgentEvent {
	return w.executor.Execute(ctx, req, w.handlers[req.ToolName])
}

func (w *WriteTools) Handler(name string) (HandlerFunc, bool) {
	handler, ok := w.handlers[name]
	return handler, ok
}

func (w *WriteTools) bindEinoTools() {
	if w.executor == nil || w.executor.registry == nil {
		return
	}
	for name := range w.handlers {
		base, err := w.executor.registry.Tool(name)
		if err != nil {
			continue
		}
		w.executor.registry.tools[name] = &writeInvokableTool{name: name, base: base, writeTools: w}
	}
}

type writeInvokableTool struct {
	name       string
	base       einotool.InvokableTool
	writeTools *WriteTools
}

func (t *writeInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.base.Info(ctx)
}

func (t *writeInvokableTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	// 获取工具执行上下文
	execution, ok := ctx.Value(toolExecutionContextKey{}).(ToolExecutionContext)
	if !ok || execution.UserID == 0 {
		return "", ErrToolExecutionContext
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		parseErr := fmt.Errorf("%w: invalid JSON arguments: %v", ErrInvalidToolArguments, err)
		event := t.writeTools.executor.Reject(ctx, executeRequestFromContext(execution, t.name, nil), parseErr)
		return event.DataJSON, fmt.Errorf("%w: %s", ErrWriteToolExecution, event.Content)
	}
	event := t.writeTools.Execute(ctx, executeRequestFromContext(execution, t.name, args))
	if event.Status != toolStatusSuccess {
		return event.DataJSON, fmt.Errorf("%w: %s", ErrWriteToolExecution, event.Content)
	}
	return event.DataJSON, nil
}
