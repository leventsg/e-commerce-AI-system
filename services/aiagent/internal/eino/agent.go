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
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	agentprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/agent"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultAgentMaxIterations = 8
	supervisorAgentName       = "supervisor_agent"
)

type RunRequest struct {
	UserID           uint64
	ConversationID   string
	MessageID        string
	RunID            string
	CheckpointID     string
	CurrentMessageID string
	ClientMessageID  string
	ClientIP         string
	AccessToken      string
	RefreshToken     string
	RAGContext       []domain.ContextMessage
	RAGSources       []domain.AgentSource
	Messages         []domain.ContextMessage
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
	AccessToken    string
	RefreshToken   string
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
	// memoryProvider 提供会话记忆功能
	memoryProvider aimemory.MemoryProvider
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

func WithMemoryProvider(provider aimemory.MemoryProvider) SupervisorOption {
	return func(opts *supervisorOptions) {
		opts.memoryProvider = provider
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
		description: agentprompt.ProductAgentDesc,
		instruction: agentprompt.ProductAgentSystemPrompt,
		tools:       []string{domain.ToolProductSearch, domain.ToolProductDetail, domain.ToolProductRecommend, domain.ToolInventoryGet},
	},
	{
		name:        "order_agent",
		description: agentprompt.OrderAgentDesc,
		instruction: agentprompt.OrderAgentSystemPrompt,
		tools:       []string{domain.ToolOrderGet, domain.ToolOrderList, domain.ToolOrderCancel},
	},
	{
		name:        "cart_checkout_agent",
		description: agentprompt.CartCheckoutAgentDesc,
		instruction: agentprompt.CartCheckoutAgentSystemPrompt,
		tools: []string{
			domain.ToolCartList, domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCartDelete,
			domain.ToolCheckoutPrepare, domain.ToolCheckoutDetail, domain.ToolOrderCreate,
		},
	},
	{
		name:        "coupon_agent",
		description: agentprompt.CouponAgentDesc,
		instruction: agentprompt.CouponAgentSystemPrompt,
		tools: []string{
			domain.ToolCouponList, domain.ToolCouponDetail, domain.ToolCouponClaim,
			domain.ToolCouponMyList, domain.ToolCouponUsageList, domain.ToolCouponCalculate,
		},
	},
	{
		name:        "general_agent",
		description: agentprompt.GeneralAgentDesc,
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
	// 创建子agent
	for _, spec := range supervisorSubAgentSpecs {
		subAgent, err := newDomainAgent(ctx, factory, cfg, registry, opts.approvalManager, spec)
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, adk.NewAgentTool(ctx, subAgent))
	}
	agentTools = append(agentTools, invokableToolsToBaseTools(registry.RootAgentTools())...)
	agentTools = append(agentTools, invokableToolsToBaseTools(registry.AllAgentTools())...)
	// 获取子agent的元信息
	agentToolInfos, err := baseToolInfos(ctx, agentTools)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	// 创建supervisor llm model
	supervisorModel, err := newAgentChatModel(ctx, factory, cfg, agentToolInfos)
	if err != nil {
		return nil, err
	}
	// 加入上下文记忆组装中间件
	rootHandlers := []adk.ChatModelAgentMiddleware{adk.NewEventSenderModelWrapper(), newToolCallContextMiddleware()}
	if opts.memoryProvider != nil {
		rootHandlers = append(rootHandlers, NewMemoryMiddleware(opts.memoryProvider))
	}
	// 创建supervisor agent
	root, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        supervisorAgentName,
		Description: agentprompt.SupervisorAgentDesc,
		Instruction: agentprompt.SupervisorSystemPrompt,
		Model:       supervisorModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: agentTools,
			},
			EmitInternalEvents: true, // 透传子agent内部事件
		},
		Handlers:      rootHandlers,
		MaxIterations: defaultAgentMaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	return &agent{root: root, checkpointStore: opts.checkpointStore, approvalManager: opts.approvalManager}, nil
}

// newDomainAgent 创建子agent
func newDomainAgent(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, registry *aitools.Registry, approvalManager *aitools.ApprovalManager, spec agentSpec) (adk.Agent, error) {
	// 这里获取的是子 agent 内部可调用的业务工具 schema；子 agent 自身作为 root 工具的 ToolInfo 由 adk.NewAgentTool 根据 Name/Description 生成。
	infos, err := registry.SubAgentToolInfos(ctx, spec.tools...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	model, err := newAgentChatModel(ctx, factory, cfg, infos)
	if err != nil {
		return nil, err
	}
	tools, err := registry.SubAgentTools(spec.tools...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	handlers := []adk.ChatModelAgentMiddleware{adk.NewEventSenderModelWrapper(), newToolCallContextMiddleware()}
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
	req.RunID = runID
	req.CheckpointID = checkpointID
	ctx = helper.WithToolExecutionContext(ctx, helper.ToolExecutionContext{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		ClientIP:       req.ClientIP,
		AccessToken:    req.AccessToken,
		RefreshToken:   req.RefreshToken,
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
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: r.root, EnableStreaming: true, CheckPointStore: store}).Run(ctx, input,
		adk.WithCheckPointID(checkpointID),
		// adk.WithSessionValues is the fixed Eino API; this project only stores trusted conversationID metadata in it.
		adk.WithSessionValues(aimemory.NewConversationValues(aimemory.ConversationMetadata{
			UserID:           req.UserID,
			ConversationID:   req.ConversationID,
			RunID:            runID,
			CurrentMessageID: req.CurrentMessageID,
			ClientMessageID:  req.ClientMessageID,
			RAGContext:       req.RAGContext,
		})))
	go func() {
		defer close(out)
		r.consumeEvents(ctx, iter, req, emit)
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
	req.RunID = runID
	req.CheckpointID = checkpointID
	logx.Infow("ai agent resume start", append(baseResumeLogFields(req), logx.Field("stage", "resume_start"))...)
	ctx = helper.WithToolExecutionContext(ctx, helper.ToolExecutionContext{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		ClientIP:       req.ClientIP,
		AccessToken:    req.AccessToken,
		RefreshToken:   req.RefreshToken,
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
		RunID:          runID,
		CheckpointID:   checkpointID,
		ClientIP:       req.ClientIP,
		AccessToken:    req.AccessToken,
		RefreshToken:   req.RefreshToken,
	}
	iter, err := adk.NewRunner(ctx, adk.RunnerConfig{Agent: r.root, EnableStreaming: true, CheckPointStore: store}).ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{
		Targets: map[string]any{
			interruptID: &ApprovalResult{Approved: req.Approved},
		},
	},
		// WithSessionValues is the fixed Eino API; this project only stores trusted conversationID metadata in it.
		adk.WithSessionValues(aimemory.NewConversationValues(aimemory.ConversationMetadata{
			UserID:         req.UserID,
			ConversationID: req.ConversationID,
			RunID:          runID,
		})))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	go func() {
		defer close(out)
		r.consumeEvents(ctx, iter, runReq, emit)
	}()
	return out, nil
}

func (r *agent) checkpointStoreOrInit() adk.CheckPointStore {
	if r.checkpointStore == nil {
		r.checkpointStore = newMemoryCheckpointStore()
	}
	return r.checkpointStore
}

func (r *agent) consumeEvents(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent], req RunRequest, emit func(context.Context, domain.AgentEvent) error) {
	hasAssistant := false
	hasAny := false
	businessExecuted := false
	previousAgentName := ""
	toolMessageIDs := make(map[string]string)
	send := func(event domain.AgentEvent) error {
		hasAny = true
		if event.Type == domain.EventAssistantMessage || event.Type == domain.EventAssistantDelta {
			hasAssistant = true
		}
		if emit == nil {
			return nil
		}
		return emit(ctx, event)
	}
	startToolCall := func(toolCallID, toolName string) (string, bool) {
		key := toolCallKey(toolCallID, toolName)
		if key == "" {
			return newAgentMessageID(), true
		}
		if existing := toolMessageIDs[key]; existing != "" {
			return existing, false
		}
		messageID := newAgentMessageID()
		toolMessageIDs[key] = messageID
		return messageID, true
	}
	finishToolCall := func(toolCallID, toolName string) (string, bool) {
		key := toolCallKey(toolCallID, toolName)
		if key == "" {
			return newAgentMessageID(), false
		}
		messageID := toolMessageIDs[key]
		if messageID == "" {
			return newAgentMessageID(), false
		}
		delete(toolMessageIDs, key)
		return messageID, true
	}
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		agentName := agentNameForLog(event.AgentName)
		if agentName != previousAgentName {
			logx.Infow("agent 切换", append(baseAgentLogFields(req), logx.Field("stage", "agent_transition"), logx.Field("agent_name", agentName), logx.Field("previous_agent_name", previousAgentName))...)
			previousAgentName = agentName
		}
		if event.Err != nil {
			logx.Errorw("ai agent iterator error", append(baseAgentLogFields(req), logx.Field("agent_name", agentName), logx.Field("err", event.Err))...)
			content := fmt.Sprintf("AI 服务暂时不可用，请稍后重试：%v", event.Err)
			dataJSON := ""
			if businessExecuted {
				content = fmt.Sprintf("业务结果已产生，但模型总结失败，请勿重复操作：%v", event.Err)
				dataJSON = `{"business_executed":true}`
			}
			_ = send(domain.AgentEvent{
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
				logx.Errorw("ai agent confirmation creation failed", append(baseAgentLogFields(req), logx.Field("stage", "confirmation_required"), logx.Field("agent_name", agentName), logx.Field("err", err))...)
				_ = send(domain.AgentEvent{
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
				logx.Infow("触发中断", append(baseAgentLogFields(req), logx.Field("agent_name", agentName), logx.Field("confirmation_id", domainEvent.ConfirmationID), logx.Field("tool", domainEvent.Tool), logx.Field("confirmation_data", domainEvent.DataJSON))...)
				_ = send(domainEvent)
			}
			return
		}
		hasAny = true
		_, stop := r.handleIteratorAgentEvent(event, req, send, startToolCall, finishToolCall, func() {
			businessExecuted = true
		})
		if stop {
			return
		}
	}
	// 如果没有任务事件发生，则发送空响应错误事件
	if !hasAssistant && !hasAny {
		_ = send(domain.AgentEvent{
			Type:           domain.EventError,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			Content:        ErrEmptyModelResponse.Error(),
			Status:         "failed",
			Done:           true,
		})
	}
}

func (r *agent) handleIteratorAgentEvent(event *adk.AgentEvent, req RunRequest, send func(domain.AgentEvent) error, startToolCall func(string, string) (string, bool), finishToolCall func(string, string) (string, bool), markBusinessExecuted func()) (handled bool, stop bool) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil {
		return false, false
	}
	message, _, err := adk.GetMessage(event)
	if err != nil {
		_ = send(domain.AgentEvent{
			Type:           domain.EventError,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			Content:        fmt.Sprintf("模型/工具事件读取失败：%v", err),
			Status:         "failed",
			Done:           true,
		})
		return true, true
	}
	if message == nil {
		return false, false
	}
	output := event.Output.MessageOutput
	role := output.Role
	if role == "" {
		role = message.Role
	}
	switch role {
	case schema.Assistant:
		return r.handleIteratorAssistantMessage(event, req, send, startToolCall, message)
	case schema.Tool:
		return r.handleIteratorToolMessage(event, req, send, finishToolCall, markBusinessExecuted, message)
	default:
		return false, false
	}
}

func (r *agent) handleIteratorAssistantMessage(event *adk.AgentEvent, req RunRequest, send func(domain.AgentEvent) error, startToolCall func(string, string) (string, bool), message *schema.Message) (handled bool, stop bool) {
	if message == nil {
		return false, false
	}
	if shouldExposeIteratorReasoning(event.AgentName) {
		if reasoning := reasoningContent(message); reasoning != "" {
			logx.Infow("ai agent reasoning event", append(baseAgentLogFields(req), logx.Field("stage", "assistant_thinking"), logx.Field("agent_name", agentNameForLog(event.AgentName)), logx.Field("reasoning_length", len(reasoning)))...)
			_ = send(domain.AgentEvent{
				Type:           domain.EventAssistantThinkingDelta,
				ConversationID: req.ConversationID,
				Content:        reasoning,
				Done:           false,
			})
			handled = true
		}
	}
	if len(message.ToolCalls) > 0 {
		for _, toolCall := range message.ToolCalls {
			toolName := strings.TrimSpace(toolCall.Function.Name)
			if toolName == "" || isAgentToolName(toolName) {
				continue
			}
			logx.Infow("触发工具调用",
				append(baseAgentLogFields(req),
					logx.Field("agent_name", agentNameForLog(event.AgentName)),
					logx.Field("tool_call_id", toolCall.ID),
					logx.Field("tool", toolName),
					logx.Field("arguments", toolCall.Function.Arguments),
				)...,
			)
			messageID, started := startToolCall(toolCall.ID, toolName)
			if !started {
				continue
			}
			_ = send(domain.AgentEvent{
				Type:           domain.EventToolProgress,
				ConversationID: req.ConversationID,
				MessageID:      messageID,
				Tool:           toolName,
				Status:         "running",
				Content:        toolProgressContent(toolName),
				DataJSON:       ensureJSONObject(toolCall.Function.Arguments),
				Done:           false,
			})
			handled = true
		}
		return handled, false
	}
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return handled, false
	}
	if event.AgentName != "" && event.AgentName != supervisorAgentName {
		return handled, false
	}
	logx.Infow("发送最终助手消息", append(baseAgentLogFields(req), logx.Field("stage", "assistant_message"), logx.Field("agent_name", agentNameForLog(event.AgentName)), logx.Field("content_length", len(content)))...)
	_ = send(domain.AgentEvent{
		Type:           domain.EventAssistantMessage,
		ConversationID: req.ConversationID,
		MessageID:      newAgentMessageID(),
		Content:        content,
		DataJSON:       sourcesDataJSON(req.RAGSources),
		Done:           true,
	})
	return true, false
}

func (r *agent) handleIteratorToolMessage(event *adk.AgentEvent, req RunRequest, send func(domain.AgentEvent) error, finishToolCall func(string, string) (string, bool), markBusinessExecuted func(), message *schema.Message) (handled bool, stop bool) {
	if message == nil {
		return false, false
	}
	toolName := strings.TrimSpace(event.Output.MessageOutput.ToolName)
	if toolName == "" {
		toolName = strings.TrimSpace(message.ToolName)
	}
	if toolName == "" || isAgentToolName(toolName) {
		return false, false
	}
	content := message.Content
	envelope, hasEnvelope := parseIteratorToolEnvelope(content)
	status := "success"
	businessExecuted := false
	if hasEnvelope {
		if envelope.ToolName != "" {
			toolName = strings.TrimSpace(envelope.ToolName)
		}
		status = strings.TrimSpace(envelope.Status)
		businessExecuted = envelope.BusinessOutcome == "executed"
	} else {
		businessExecuted = isBusinessWriteTool(toolName) || (r.approvalManager != nil && r.approvalManager.RequiresConfirmation(toolName))
	}
	messageID, _ := finishToolCall(message.ToolCallID, toolName)
	logx.Infow("发送工具调用结果",
		logx.Field("agent_name", agentNameForLog(event.AgentName)),
		logx.Field("tool_call_id", message.ToolCallID),
		logx.Field("tool", toolName),
		logx.Field("status", status),
		logx.Field("business_executed", businessExecuted),
	)
	if businessExecuted {
		markBusinessExecuted()
	}
	_ = send(domain.AgentEvent{
		Type:             domain.EventToolResult,
		ConversationID:   req.ConversationID,
		MessageID:        messageID,
		Tool:             toolName,
		Status:           status,
		Content:          iteratorToolResultSummary(toolName, content, envelope),
		DataJSON:         ensureJSONObject(content),
		Done:             true,
		BusinessExecuted: businessExecuted,
	})
	return true, false
}

func shouldExposeIteratorReasoning(agentName string) bool {
	agentName = strings.TrimSpace(agentName)
	return agentName == "" || agentName == supervisorAgentName || !isKnownAgentName(agentName)
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

func baseAgentLogFields(req RunRequest) []logx.LogField {
	return []logx.LogField{
		logx.Field("component", "supervisor_agent"),
		logx.Field("conversation_id", req.ConversationID),
		logx.Field("user_id", req.UserID),
		logx.Field("run_id", req.RunID),
		logx.Field("checkpoint_id", req.CheckpointID),
		logx.Field("client_message_id", req.ClientMessageID),
	}
}

func baseResumeLogFields(req ResumeRequest) []logx.LogField {
	return []logx.LogField{
		logx.Field("component", "supervisor_agent"),
		logx.Field("conversation_id", req.ConversationID),
		logx.Field("user_id", req.UserID),
		logx.Field("confirmation_id", req.ConfirmationID),
		logx.Field("run_id", req.RunID),
		logx.Field("checkpoint_id", req.CheckpointID),
		logx.Field("interrupt_id", req.InterruptID),
		logx.Field("approved", req.Approved),
	}
}

func agentNameForLog(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
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
