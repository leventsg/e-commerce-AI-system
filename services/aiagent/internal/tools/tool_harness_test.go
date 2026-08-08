package tools

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type toolTestHarness struct {
	registry *Registry
	executor *Executor
}

func newTestToolHarness(clients DefaultToolClients, opts ...ExecutorOption) *toolTestHarness {
	registry := NewRegistry(DefaultTools(clients, config.ToolTimeoutConfig{}))
	executor := NewExecutor(registry, opts...)
	return &toolTestHarness{registry: registry, executor: executor}
}

func newTestRegistry(clients DefaultToolClients, timeout config.ToolTimeoutConfig) *Registry {
	return NewRegistry(DefaultTools(clients, timeout))
}

func newTestApprovalManager(clients DefaultToolClients, creator ConfirmationCreator, opts ...ExecutorOption) *ApprovalManager {
	registry := newTestRegistry(clients, config.ToolTimeoutConfig{})
	executor := NewExecutor(registry, opts...)
	return NewApprovalManager(registry, executor, creator)
}

func (h *toolTestHarness) Execute(ctx context.Context, req ExecuteRequest) domain.AgentEvent {
	handler, _ := h.registry.Handler(req.ToolName)
	return h.executor.Execute(ctx, req, handler)
}

func (h *toolTestHarness) Handler(name string) (HandlerFunc, bool) {
	return h.registry.Handler(name)
}

var _ ConfirmationCreator = (*fakeConfirmationCreator)(nil)
