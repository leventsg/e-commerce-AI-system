package eino

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"google.golang.org/grpc"
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
		t.Fatalf("event = %+v, want product_search tool result", event)
	}
}

func TestDomainAgentInterruptsHighRiskToolWithConfirmationID(t *testing.T) {
	registry := aitools.NewRegistry(config.ToolTimeoutConfig{})
	highRisk := aitools.NewHighRiskTools(aitools.NewExecutor(registry), &fixedConfirmationCreator{}, aitools.HighRiskToolClients{})
	model := &capturingToolCallingChatModel{responses: []*schema.Message{
		schema.AssistantMessage("需要确认", []schema.ToolCall{{
			ID:       "call-confirm",
			Function: schema.FunctionCall{Name: domain.ToolOrderCancel, Arguments: `{"order_id":"order-1","reason":"不想买了"}`},
		}}),
	}}
	root, err := newDomainAgent(context.Background(), &singleModelFactory{model: model}, config.EinoConfig{}, registry, highRisk, agentSpec{
		name:        "order_agent",
		description: "test order agent",
		instruction: "test",
		tools:       []string{domain.ToolOrderCancel},
	})
	if err != nil {
		t.Fatalf("newDomainAgent() error = %v", err)
	}

	runner := &agent{root: root, checkpointStore: newMemoryCheckpointStore(), highRiskTools: highRisk}
	events, err := runner.Run(context.Background(), RunRequest{
		UserID:         42,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		Messages:       []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "取消订单 order-1"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one confirmation event", events)
	}
	event := events[0]
	if event.Type != domain.EventConfirmationRequired || event.ConfirmationID != "confirm-1" || event.Tool != domain.ToolOrderCancel {
		t.Fatalf("event = %+v, want order_cancel confirmation", event)
	}
	if event.Status != confirmation.StatusPending || event.ExpiresAt == 0 || event.DataJSON == "" {
		t.Fatalf("event missing confirmation details: %+v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.DataJSON), &payload); err != nil {
		t.Fatalf("decode event data: %v", err)
	}
	if payload["confirmation_id"] != "confirm-1" || payload["checkpoint_id"] != "msg-1" || payload["interrupt_id"] == "" {
		t.Fatalf("event data = %#v, want confirmation and resume target", payload)
	}
}

func TestRunnerResumeApprovedExecutesOriginalHighRiskTool(t *testing.T) {
	registry := aitools.NewRegistry(config.ToolTimeoutConfig{})
	creator := &fixedConfirmationCreator{}
	cartRPC := &fakeEinoCartHighRiskRPC{
		listResp:   &cartsclient.CartItemListResponse{Data: []*cartsclient.CartInfoResponse{{Id: 8, UserId: 42, ProductId: 11, Quantity: 2}}},
		deleteResp: &cartsclient.EmptyCartResponse{},
	}
	highRisk := aitools.NewHighRiskTools(aitools.NewExecutor(registry), creator, aitools.HighRiskToolClients{Cart: cartRPC})
	model := &capturingToolCallingChatModel{responses: []*schema.Message{
		schema.AssistantMessage("需要确认", []schema.ToolCall{{
			ID:       "call-confirm",
			Function: schema.FunctionCall{Name: domain.ToolCartDelete, Arguments: `{"cart_item_id":8}`},
		}}),
		schema.AssistantMessage("购物车条目 8 已删除。", nil),
	}}
	root, err := newDomainAgent(context.Background(), &singleModelFactory{model: model}, config.EinoConfig{}, registry, highRisk, agentSpec{
		name:        "cart_checkout_agent",
		description: "test cart agent",
		instruction: "test",
		tools:       []string{domain.ToolCartDelete},
	})
	if err != nil {
		t.Fatalf("newDomainAgent() error = %v", err)
	}
	runner := &agent{root: root, checkpointStore: newMemoryCheckpointStore(), highRiskTools: highRisk}
	events, err := runner.Run(context.Background(), RunRequest{
		UserID:         42,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		Messages:       []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "删除购物车条目 8"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 || events[0].ConfirmationID != "confirm-1" {
		t.Fatalf("events = %+v, want confirmation", events)
	}
	if cartRPC.deleteCalls != 0 {
		t.Fatalf("business RPC called before resume: %d", cartRPC.deleteCalls)
	}
	if creator.bindReq.CheckpointID == "" || creator.bindReq.InterruptID == "" {
		t.Fatalf("resume target was not bound: %#v", creator.bindReq)
	}

	resumed, err := runner.Resume(context.Background(), ResumeRequest{
		UserID:         42,
		ConversationID: "conv-1",
		ConfirmationID: "confirm-1",
		RunID:          creator.bindReq.RunID,
		CheckpointID:   creator.bindReq.CheckpointID,
		InterruptID:    creator.bindReq.InterruptID,
		Approved:       true,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if cartRPC.deleteCalls != 1 {
		t.Fatalf("business RPC calls after resume = %d, want 1", cartRPC.deleteCalls)
	}
	hasBusinessEvent := false
	for _, event := range resumed {
		if event.Type == domain.EventToolResult && event.Tool == domain.ToolCartDelete && event.BusinessExecuted {
			hasBusinessEvent = true
		}
	}
	if !hasBusinessEvent {
		t.Fatalf("resumed events = %+v, want business tool result", resumed)
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

type singleModelFactory struct {
	model *capturingToolCallingChatModel
}

func (f *singleModelFactory) NewChatModel(context.Context, config.EinoConfig, ...*schema.ToolInfo) (model.BaseChatModel, error) {
	return f.model, nil
}

func (f *singleModelFactory) NewStructuredChatModel(context.Context, config.EinoConfig, StructuredOutputConfig, ...*schema.ToolInfo) (model.BaseChatModel, error) {
	return nil, errors.New("unused")
}

type fixedConfirmationCreator struct {
	bindReq confirmation.ResumeTargetRequest
}

func (f *fixedConfirmationCreator) Create(_ context.Context, req confirmation.CreateRequest) (*domain.Confirmation, error) {
	return &domain.Confirmation{
		ID:             "confirm-1",
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		ToolName:       req.ToolName,
		Arguments:      req.Arguments,
		Summary:        req.Summary,
		Status:         confirmation.StatusPending,
		RunID:          req.RunID,
		CheckpointID:   req.CheckpointID,
		ExpiresAt:      time.Unix(1893456000, 0),
	}, nil
}

func (f *fixedConfirmationCreator) BindResumeTarget(_ context.Context, req confirmation.ResumeTargetRequest) (*domain.Confirmation, error) {
	f.bindReq = req
	return &domain.Confirmation{
		ID:             req.ConfirmationID,
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		Status:         confirmation.StatusPending,
		RunID:          req.RunID,
		CheckpointID:   req.CheckpointID,
		InterruptID:    req.InterruptID,
		ExpiresAt:      time.Unix(1893456000, 0),
	}, nil
}

type fakeEinoCartHighRiskRPC struct {
	listCalls   int
	deleteCalls int
	listResp    *cartsclient.CartItemListResponse
	deleteResp  *cartsclient.EmptyCartResponse
}

func (f *fakeEinoCartHighRiskRPC) CartItemList(context.Context, *cartsclient.UserInfo, ...grpc.CallOption) (*cartsclient.CartItemListResponse, error) {
	f.listCalls++
	return f.listResp, nil
}

func (f *fakeEinoCartHighRiskRPC) DeleteCartItem(context.Context, *cartsclient.CartItemRequest, ...grpc.CallOption) (*cartsclient.EmptyCartResponse, error) {
	f.deleteCalls++
	return f.deleteResp, nil
}
