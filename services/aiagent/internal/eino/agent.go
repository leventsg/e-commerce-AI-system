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
	OnEvent        func(context.Context, domain.AgentEvent) error
}

type Runner interface {
	Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error)
	Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error)
}

type agent struct {
	root adk.Agent
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

func NewSupervisorAgent(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, registry *aitools.Registry) (Runner, error) {
	if factory == nil || registry == nil {
		return nil, ErrModelUnavailable
	}
	_ = adk.SetLanguage(adk.LanguageChinese)
	agentTools := make([]einotool.BaseTool, 0, len(supervisorSubAgentSpecs))
	for _, spec := range supervisorSubAgentSpecs {
		subAgent, err := newDomainAgent(ctx, factory, cfg, registry, spec)
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
	return &agent{root: root}, nil
}

// newDomainAgent 创建子agent
func newDomainAgent(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, registry *aitools.Registry, spec agentSpec) (adk.Agent, error) {
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
	if r == nil || r.root == nil {
		return nil, ErrModelUnavailable
	}
	input, err := buildInputMessages(req)
	if err != nil {
		return nil, err
	}
	ctx = aitools.WithToolExecutionContext(ctx, aitools.ToolExecutionContext{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		ClientIP:       req.ClientIP,
	})
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: r.root}).Run(ctx, input)
	events := make([]domain.AgentEvent, 0, 2)
	hasAssistant := false
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, event.Err)
		}
		domainEvent, ok, err := adkEventToDomainEvent(event, req)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if domainEvent.Type == domain.EventAssistantMessage {
			hasAssistant = true
		}
		if req.OnEvent != nil {
			if err := req.OnEvent(ctx, domainEvent); err != nil {
				return nil, err
			}
		}
		events = append(events, domainEvent)
	}
	if !hasAssistant {
		return nil, ErrEmptyModelResponse
	}
	return events, nil
}

func (r *agent) Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error) {
	events, err := r.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan domain.AgentEvent, len(events))
	for _, event := range events {
		out <- event
	}
	close(out)
	return out, nil
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

func adkEventToDomainEvent(event *adk.AgentEvent, req RunRequest) (domain.AgentEvent, bool, error) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil {
		return domain.AgentEvent{}, false, nil
	}
	message, _, err := adk.GetMessage(event)
	if err != nil {
		return domain.AgentEvent{}, false, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return domain.AgentEvent{}, false, nil
	}
	output := event.Output.MessageOutput
	switch output.Role {
	case schema.Assistant:
		if len(message.ToolCalls) > 0 {
			return domain.AgentEvent{}, false, nil
		}
		if event.AgentName != "" && event.AgentName != supervisorAgentName {
			return domain.AgentEvent{}, false, nil
		}
		return domain.AgentEvent{
			Type:           domain.EventAssistantMessage,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			Content:        message.Content,
			Done:           true,
		}, true, nil
	case schema.Tool:
		toolName := output.ToolName
		if toolName == "" {
			toolName = message.ToolName
		}
		if isAgentToolName(toolName) {
			return domain.AgentEvent{}, false, nil
		}
		dataJSON := message.Content
		if !jsonObjectLike(dataJSON) {
			dataJSON = fmt.Sprintf(`{"result":%q}`, message.Content)
		}
		return domain.AgentEvent{
			Type:           domain.EventToolResult,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			ToolCallID:     message.ToolCallID,
			Content:        message.Content,
			Tool:           toolName,
			Status:         "success",
			DataJSON:       dataJSON,
			Done:           true,
		}, true, nil
	default:
		return domain.AgentEvent{}, false, nil
	}
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
