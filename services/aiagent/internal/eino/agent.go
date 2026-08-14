package eino

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	agentprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/agent"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

const (
	defaultAgentMaxIterations = 8
	supervisorAgentName       = "supervisor_agent"
)

type RunRequest struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ClientIP       string
	Messages       []domain.ContextMessage
}

type ResumeRequest struct {
	UserID         uint64
	ConversationID string
	ConfirmationID string
	RunID          string
	CheckpointID   string
	InterruptID    string
	Approved       bool
	ClientIP       string
}

type Runner interface {
	Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error)
	Resume(ctx context.Context, req ResumeRequest) ([]domain.AgentEvent, error)
	Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error)
	ResumeStream(ctx context.Context, req ResumeRequest) (<-chan domain.AgentEvent, error)
}

type agent struct {
	root            adk.Agent
	checkpointStore adk.CheckPointStore
	approvalManager *aitools.ApprovalManager
}

type supervisorOptions struct {
	approvalManager *aitools.ApprovalManager
	checkpointStore adk.CheckPointStore
}

type SupervisorOption func(*supervisorOptions)

func WithApprovalManager(approvalManager *aitools.ApprovalManager) SupervisorOption {
	return func(opts *supervisorOptions) {
		opts.approvalManager = approvalManager
	}
}

func WithCheckpointStore(store adk.CheckPointStore) SupervisorOption {
	return func(opts *supervisorOptions) {
		opts.checkpointStore = store
	}
}

type agentSpec struct {
	name        string
	description string
	instruction string
	tools       []string
}

var supervisorSubAgentSpecs = []agentSpec{
	{
		name:        "product_agent",
		description: "Handles product search, product detail, product recommendation, and inventory lookup.",
		instruction: agentprompt.ProductAgentSystemPrompt,
		tools:       []string{domain.ToolProductSearch, domain.ToolProductDetail, domain.ToolProductRecommend, domain.ToolInventoryGet},
	},
	{
		name:        "order_agent",
		description: "Handles order lookup, order list, and cancel-order confirmation requests.",
		instruction: agentprompt.OrderAgentSystemPrompt,
		tools:       []string{domain.ToolOrderGet, domain.ToolOrderList, domain.ToolOrderCancel},
	},
	{
		name:        "cart_checkout_agent",
		description: "Handles cart operations, checkout preparation/detail, and create-order confirmation requests.",
		instruction: agentprompt.CartCheckoutAgentSystemPrompt,
		tools: []string{
			domain.ToolCartList, domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCartDelete,
			domain.ToolCheckoutPrepare, domain.ToolCheckoutDetail, domain.ToolOrderCreate,
		},
	},
	{
		name:        "coupon_agent",
		description: "Handles coupon discovery, coupon detail, claim, owned coupons, usage records, and discount calculation.",
		instruction: agentprompt.CouponAgentSystemPrompt,
		tools: []string{
			domain.ToolCouponList, domain.ToolCouponDetail, domain.ToolCouponClaim,
			domain.ToolCouponMyList, domain.ToolCouponUsageList, domain.ToolCouponCalculate,
		},
	},
	{
		name:        "general_agent",
		description: "Handles general customer-service explanations, small talk, and unclassified requests.",
		instruction: agentprompt.GeneralAgentSystemPrompt,
	},
}

func NewSupervisorAgent(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, registry *aitools.Registry, options ...SupervisorOption) (Runner, error) {
	if factory == nil || registry == nil {
		return nil, ErrModelUnavailable
	}
	opts := supervisorOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	_ = adk.SetLanguage(adk.LanguageChinese)
	agentTools := make([]einotool.BaseTool, 0, len(supervisorSubAgentSpecs))
	for _, spec := range supervisorSubAgentSpecs {
		subAgent, err := newDomainAgent(ctx, factory, cfg, registry, opts.approvalManager, spec)
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, adk.NewAgentTool(ctx, subAgent))
	}
	agentToolInfos, err := baseToolInfos(ctx, agentTools)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	supervisorModel, err := newAgentChatModel(ctx, factory, cfg, agentToolInfos)
	if err != nil {
		return nil, err
	}
	root, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        supervisorAgentName,
		Description: "Coordinates e-commerce customer service sub-agents, decomposes tasks, routes work, and summarizes final answers.",
		Instruction: agentprompt.SupervisorSystemPrompt,
		Model:       supervisorModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: agentTools,
			},
			EmitInternalEvents: true,
		},
		MaxIterations: defaultAgentMaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	return &agent{root: root, checkpointStore: opts.checkpointStore, approvalManager: opts.approvalManager}, nil
}

// newDomainAgent 创建子agent
func newDomainAgent(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, registry *aitools.Registry, approvalManager *aitools.ApprovalManager, spec agentSpec) (adk.Agent, error) {
	infos, err := registry.ToolInfosByNames(ctx, spec.tools...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	model, err := newAgentChatModel(ctx, factory, cfg, infos)
	if err != nil {
		return nil, err
	}
	tools, err := registry.ToolsByNames(spec.tools...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	handlers := []adk.ChatModelAgentMiddleware{}
	if approvalManager != nil {
		handlers = append(handlers, newHighRiskApprovalMiddleware(approvalManager))
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        spec.name,
		Description: spec.description,
		Instruction: spec.instruction,
		Model:       model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: invokableToolsToBaseTools(tools),
			},
		},
		Handlers:      handlers,
		MaxIterations: defaultAgentMaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	return agent, nil
}

func newAgentChatModel(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	chatModel, err := factory.NewChatModel(ctx, cfg, infos...)
	if err != nil {
		return nil, err
	}
	toolCallingModel, ok := chatModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("%w: model does not support tool calling", ErrModelUnavailable)
	}
	return toolCallingModel, nil
}

func buildInputMessages(req RunRequest) ([]*schema.Message, error) {
	return ConvertContextMessages(req.Messages)
}

func (r *agent) Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error) {
	stream, err := r.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	return collectStream(stream), nil
}

func (r *agent) Resume(ctx context.Context, req ResumeRequest) ([]domain.AgentEvent, error) {
	stream, err := r.ResumeStream(ctx, req)
	if err != nil {
		return nil, err
	}
	return collectStream(stream), nil
}

func (r *agent) Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error) {
	if r == nil || r.root == nil {
		return nil, ErrModelUnavailable
	}
	input, err := buildInputMessages(req)
	if err != nil {
		return nil, err
	}
	checkpointID := stableCheckpointID(req.MessageID, req.ConversationID)
	runID := checkpointID
	ctx = aitools.WithToolExecutionContext(ctx, aitools.ToolExecutionContext{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		ClientIP:       req.ClientIP,
		RunID:          runID,
		CheckpointID:   checkpointID,
	})
	ctx = withApprovalRunMeta(ctx, approvalRunMeta{RunID: runID, CheckpointID: checkpointID})
	store := r.checkpointStoreOrInit()
	out := make(chan domain.AgentEvent, 4)
	emit := func(eventCtx context.Context, event domain.AgentEvent) error {
		select {
		case out <- event:
			return nil
		case <-eventCtx.Done():
			return eventCtx.Err()
		}
	}
	// callback handler manager
	bridge := newAgentEventCallbackBridge(req, r.approvalManager, emit)
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: r.root, EnableStreaming: true, CheckPointStore: store}).Run(ctx, input,
		adk.WithCheckPointID(checkpointID),
		adk.WithCallbacks(bridge.modelHandler()).DesignateAgent(supervisorAgentName),
		adk.WithCallbacks(bridge.toolHandler()))
	go func() {
		defer close(out)
		r.consumeEvents(ctx, iter, req, bridge, emit)
	}()
	return out, nil
}

func (r *agent) ResumeStream(ctx context.Context, req ResumeRequest) (<-chan domain.AgentEvent, error) {
	if r == nil || r.root == nil {
		return nil, ErrModelUnavailable
	}
	checkpointID := strings.TrimSpace(req.CheckpointID)
	interruptID := strings.TrimSpace(req.InterruptID)
	if checkpointID == "" || interruptID == "" {
		return nil, fmt.Errorf("%w: checkpoint or interrupt target is empty", ErrModelUnavailable)
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = checkpointID
	}
	ctx = aitools.WithToolExecutionContext(ctx, aitools.ToolExecutionContext{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		ClientIP:       req.ClientIP,
		RunID:          runID,
		CheckpointID:   checkpointID,
	})
	ctx = withApprovalRunMeta(ctx, approvalRunMeta{RunID: runID, CheckpointID: checkpointID})
	store := r.checkpointStoreOrInit()
	out := make(chan domain.AgentEvent, 4)
	// 事件发送器
	emit := func(eventCtx context.Context, event domain.AgentEvent) error {
		select {
		case out <- event:
			return nil
		case <-eventCtx.Done():
			return eventCtx.Err()
		}
	}
	runReq := RunRequest{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		MessageID:      req.ConfirmationID,
		ClientIP:       req.ClientIP,
	}
	// 桥接器，跟踪业务执行状态
	bridge := newAgentEventCallbackBridge(runReq, r.approvalManager, emit)
	iter, err := adk.NewRunner(ctx, adk.RunnerConfig{Agent: r.root, EnableStreaming: true, CheckPointStore: store}).ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{
		Targets: map[string]any{
			interruptID: &ApprovalResult{Approved: req.Approved},
		},
	},
		adk.WithCallbacks(bridge.modelHandler()).DesignateAgent(supervisorAgentName),
		adk.WithCallbacks(bridge.toolHandler()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	go func() {
		defer close(out)
		r.consumeEvents(ctx, iter, runReq, bridge, emit)
	}()
	return out, nil
}

func (r *agent) checkpointStoreOrInit() adk.CheckPointStore {
	if r.checkpointStore == nil {
		r.checkpointStore = newMemoryCheckpointStore()
	}
	return r.checkpointStore
}

func (r *agent) consumeEvents(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent], req RunRequest, bridge *agentEventCallbackBridge, emit func(context.Context, domain.AgentEvent) error) {
	// 是否收到assistant消息事件
	hasAssistant := false
	// 是否收到中断事件
	hasInterrupt := false
	// 是否收到任何事件
	hasAny := false
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			businessExecuted := bridge != nil && bridge.hasBusinessExecuted()
			content := fmt.Sprintf("AI 服务暂时不可用，请稍后重试：%v", event.Err)
			dataJSON := ""
			if businessExecuted {
				content = fmt.Sprintf("业务结果已产生，但模型总结失败，请勿重复操作：%v", event.Err)
				dataJSON = `{"business_executed":true}`
			}
			_ = emit(ctx, domain.AgentEvent{
				Type:             domain.EventError,
				ConversationID:   req.ConversationID,
				MessageID:        newAgentMessageID(),
				Content:          content,
				Status:           "failed",
				DataJSON:         dataJSON,
				Done:             true,
				BusinessExecuted: businessExecuted,
			})
			return
		}
		// 中断事件处理
		if event.Action != nil && event.Action.Interrupted != nil {
			// 将中断事件转换为自定义AgentEvent事件
			domainEvent, ok, err := interruptEventToDomainEvent(ctx, event.Action.Interrupted, req, r.approvalManager)
			if err != nil {
				_ = emit(ctx, domain.AgentEvent{
					Type:           domain.EventError,
					ConversationID: req.ConversationID,
					MessageID:      newAgentMessageID(),
					Content:        fmt.Sprintf("确认请求创建失败：%v", err),
					Status:         "failed",
					Done:           true,
				})
				return
			}
			if ok {
				hasInterrupt = true
				if bridge != nil {
					if normalized, normalizedOK := bridge.enterAwaitingConfirmation(domainEvent); normalizedOK {
						domainEvent = normalized
					}
				}
				_ = emit(ctx, domainEvent)
			}
			return
		}
		hasAny = true
		domainEvent, ok := iteratorAssistantEventToDomainEvent(event, req)
		if !ok {
			continue
		}
		hasAssistant = true
		_ = emit(ctx, domainEvent)
	}
	if bridge != nil {
		hasAssistant = hasAssistant || bridge.hasAssistantEvent()
		hasAny = hasAny || bridge.hasAnyEvent()
	}
	// 如果没有任务事件发生，则发送空响应错误事件
	if !hasAssistant && !hasInterrupt && !hasAny {
		_ = emit(ctx, domain.AgentEvent{
			Type:           domain.EventError,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			Content:        ErrEmptyModelResponse.Error(),
			Status:         "failed",
			Done:           true,
		})
	}
}

// 从迭代器获取最终的assistant消息
func iteratorAssistantEventToDomainEvent(event *adk.AgentEvent, req RunRequest) (domain.AgentEvent, bool) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil {
		return domain.AgentEvent{}, false
	}
	message, _, err := adk.GetMessage(event)
	if err != nil || message == nil || strings.TrimSpace(message.Content) == "" {
		return domain.AgentEvent{}, false
	}
	output := event.Output.MessageOutput
	// 过滤role
	if output.Role != schema.Assistant {
		return domain.AgentEvent{}, false
	}
	// 如果有工具调用，则说明不是最终消息
	if len(message.ToolCalls) > 0 {
		return domain.AgentEvent{}, false
	}
	// 过滤子 agent 的最终 assistant 消息
	if event.AgentName != "" && event.AgentName != supervisorAgentName {
		return domain.AgentEvent{}, false
	}
	return domain.AgentEvent{
		Type:           domain.EventAssistantMessage,
		ConversationID: req.ConversationID,
		MessageID:      newAgentMessageID(),
		Content:        message.Content,
		Done:           true,
	}, true
}

func collectStream(stream <-chan domain.AgentEvent) []domain.AgentEvent {
	events := make([]domain.AgentEvent, 0, 2)
	for event := range stream {
		events = append(events, event)
	}
	return events
}

func invokableToolsToBaseTools(tools []einotool.InvokableTool) []einotool.BaseTool {
	result := make([]einotool.BaseTool, 0, len(tools))
	for _, item := range tools {
		if item != nil {
			result = append(result, item)
		}
	}
	return result
}

func baseToolInfos(ctx context.Context, tools []einotool.BaseTool) ([]*schema.ToolInfo, error) {
	result := make([]*schema.ToolInfo, 0, len(tools))
	for _, item := range tools {
		if item == nil {
			continue
		}
		info, err := item.Info(ctx)
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, nil
}

func stableCheckpointID(messageID, conversationID string) string {
	checkpointID := strings.TrimSpace(messageID)
	if checkpointID == "" {
		checkpointID = strings.TrimSpace(conversationID)
	}
	return checkpointID
}

func isAgentToolName(toolName string) bool {
	for _, spec := range supervisorSubAgentSpecs {
		if toolName == spec.name {
			return true
		}
	}
	return false
}

func jsonObjectLike(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}")
}

func newAgentMessageID() string {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return "msg_" + id.String()
}
