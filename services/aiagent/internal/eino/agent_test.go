package eino

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

func TestNewSupervisorAgentBuildsDomainAgentsWithScopedTools(t *testing.T) {
	registry := aitools.NewRegistry(config.ToolTimeoutConfig{})
	factory := &capturingSupervisorModelFactory{}

	runner, err := NewSupervisorAgent(context.Background(), factory, config.EinoConfig{Provider: "deepseek", Model: "fast"}, registry)
	if err != nil {
		t.Fatalf("NewSupervisorAgent() error = %v", err)
	}
	if runner == nil {
		t.Fatal("NewSupervisorAgent() returned nil runner")
	}
	if len(factory.models) != 6 {
		t.Fatalf("model count = %d, want supervisor + 5 sub-agents", len(factory.models))
	}
	assertBoundToolNames(t, factory.models[0], []string{domain.ToolProductSearch, domain.ToolProductDetail, domain.ToolProductRecommend, domain.ToolInventoryGet})
	assertBoundToolNames(t, factory.models[1], []string{domain.ToolOrderGet, domain.ToolOrderList, domain.ToolOrderCancel})
	assertBoundToolNames(t, factory.models[2], []string{domain.ToolCartList, domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCartDelete, domain.ToolCheckoutPrepare, domain.ToolCheckoutDetail, domain.ToolOrderCreate})
	assertBoundToolNames(t, factory.models[3], []string{domain.ToolCouponList, domain.ToolCouponDetail, domain.ToolCouponClaim, domain.ToolCouponMyList, domain.ToolCouponUsageList, domain.ToolCouponCalculate})
	assertBoundToolNames(t, factory.models[4], nil)
	assertBoundToolNames(t, factory.models[5], []string{"product_agent", "order_agent", "cart_checkout_agent", "coupon_agent", "general_agent"})
}

func TestADKEventConversionSkipsAgentToolWrapperAndKeepsBusinessToolEvents(t *testing.T) {
	wrapperMessage := schema.ToolMessage("product agent done", "call_agent", schema.WithToolName("product_agent"))
	wrapperEvent := adk.EventFromMessage(wrapperMessage, nil, schema.Tool, wrapperMessage.ToolName)
	if _, ok, err := adkEventToDomainEvent(wrapperEvent, RunRequest{ConversationID: "conv-1"}); err != nil || ok {
		t.Fatalf("agent tool wrapper conversion ok=%v err=%v, want skipped", ok, err)
	}

	businessMessage := schema.ToolMessage(`{"items":[]}`, "call_tool", schema.WithToolName(domain.ToolProductSearch))
	businessEvent := adk.EventFromMessage(businessMessage, nil, schema.Tool, businessMessage.ToolName)
	event, ok, err := adkEventToDomainEvent(businessEvent, RunRequest{ConversationID: "conv-1"})
	if err != nil || !ok {
		t.Fatalf("business tool conversion ok=%v err=%v, want converted", ok, err)
	}
	if event.Type != domain.EventToolResult || event.Tool != domain.ToolProductSearch {
		t.Fatalf("event = %+v, want product.search tool result", event)
	}
}

func assertBoundToolNames(t *testing.T, model *capturingToolCallingChatModel, want []string) {
	t.Helper()
	if len(model.boundTools) != len(want) {
		t.Fatalf("bound tools = %+v, want %v", toolNames(model.boundTools), want)
	}
	for i, item := range want {
		if model.boundTools[i].Name != item {
			t.Fatalf("bound tools = %+v, want %v", toolNames(model.boundTools), want)
		}
	}
}

func toolNames(infos []*schema.ToolInfo) []string {
	result := make([]string, 0, len(infos))
	for _, info := range infos {
		if info != nil {
			result = append(result, info.Name)
		}
	}
	return result
}

type capturingSupervisorModelFactory struct {
	models []*capturingToolCallingChatModel
}

func (f *capturingSupervisorModelFactory) NewChatModel(_ context.Context, _ config.EinoConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error) {
	chatModel := &capturingToolCallingChatModel{capturingChatModel: capturingChatModel{response: schema.AssistantMessage("ok", nil)}}
	if len(tools) > 0 {
		chatModel.boundTools = tools
	}
	f.models = append(f.models, chatModel)
	return chatModel, nil
}

func (f *capturingSupervisorModelFactory) NewStructuredChatModel(context.Context, config.EinoConfig, StructuredOutputConfig, ...*schema.ToolInfo) (model.BaseChatModel, error) {
	return nil, errors.New("unused")
}

type emptyResponseModel struct{ response *schema.Message }

func (m emptyResponseModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.response, nil
}
func (m emptyResponseModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingChatModel struct {
	response *schema.Message
	messages []*schema.Message
}

func (m *capturingChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.messages = messages
	return m.response, nil
}

func (m *capturingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingToolCallingChatModel struct {
	capturingChatModel
	responses  []*schema.Message
	boundTools []*schema.ToolInfo
}

func (m *capturingToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.boundTools = tools
	return m, nil
}

func (m *capturingToolCallingChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.messages = append([]*schema.Message(nil), messages...)
	options := model.GetCommonOptions(&model.Options{}, opts...)
	if options.Tools != nil {
		m.boundTools = options.Tools
	}
	if len(m.responses) > 0 {
		response := m.responses[0]
		m.responses = m.responses[1:]
		return response, nil
	}
	return m.capturingChatModel.Generate(ctx, messages, opts...)
}

func (m *capturingToolCallingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingInvokableTool struct {
	info      *schema.ToolInfo
	result    string
	calls     int
	arguments string
	execution any
}

func (t *capturingInvokableTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *capturingInvokableTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	t.calls++
	t.arguments = arguments
	execution, _ := aitools.ToolExecutionFromContext(ctx)
	t.execution = execution
	return t.result, nil
}
