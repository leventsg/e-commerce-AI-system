package planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

func TestPlannerUsesLLMResultBeforeRules(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"recommend","tool_name":"product.recommend","arguments":{"query":"LLM extracted need"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "推荐几款适合学生党的手机"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.Intent != IntentRecommend {
		t.Fatalf("Intent = %q, want %q", result.Intent, IntentRecommend)
	}
	if result.ToolName != domain.ToolProductRecommend {
		t.Fatalf("ToolName = %q, want %q", result.ToolName, domain.ToolProductRecommend)
	}
	assertArguments(t, result.Arguments, map[string]any{"query": "LLM extracted need"})
}

func TestPlannerLLMHighRiskConfirmationComesFromRegistry(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"order.cancel","arguments":{"order_id":"202406300001"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "取消订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if !result.RequireConfirmation {
		t.Fatal("RequireConfirmation = false, want true from registry metadata")
	}
}

func TestPlannerRemovesUserIDFromLLMArguments(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"order.cancel","arguments":{"order_id":"202406300001","user_id":999}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "取消订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if _, ok := result.Arguments["user_id"]; ok {
		t.Fatalf("Arguments exposes user_id: %#v", result.Arguments)
	}
}

func TestPlannerRetriesLLMErrorThenUsesSecondResult(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{err: errors.New("temporary model error")},
		{content: `{"intent":"query","tool_name":"order.get","arguments":{"order_id":"202406300001"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "查一下订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.ToolName != domain.ToolOrderGet {
		t.Fatalf("ToolName = %q, want %q", result.ToolName, domain.ToolOrderGet)
	}
	assertArguments(t, result.Arguments, map[string]any{"order_id": "202406300001"})
}

func TestPlannerRetriesInvalidJSONThenUsesSecondResult(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `not json`},
		{content: `{"intent":"query","tool_name":"order.get","arguments":{"order_id":"202406300001"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "查一下订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.ToolName != domain.ToolOrderGet {
		t.Fatalf("ToolName = %q, want %q", result.ToolName, domain.ToolOrderGet)
	}
}

func TestPlannerFallsBackAfterTwoLLMFailures(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{err: errors.New("first failure")},
		{err: errors.New("second failure")},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "查一下订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.Intent != IntentQuery || result.ToolName != domain.ToolOrderGet {
		t.Fatalf("Plan = %#v, want query order.get fallback", result)
	}
	assertArguments(t, result.Arguments, map[string]any{"order_id": "202406300001"})
}

func TestPlannerFallsBackAfterTwoUnknownLLMTools(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"query","tool_name":"unknown.tool","arguments":{"order_id":"202406300001"}}`},
		{content: `{"intent":"query","tool_name":"still.unknown","arguments":{"order_id":"202406300001"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "查一下订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.Intent != IntentQuery || result.ToolName != domain.ToolOrderGet {
		t.Fatalf("Plan = %#v, want query order.get fallback", result)
	}
}

func TestPlannerLLMReceivesPrebuiltContextWithoutCropping(t *testing.T) {
	planner, fakeModel := newPlannerAndFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"cart.add","arguments":{"product_id":12,"quantity":2}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{
		Message: "加两件",
		Messages: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "意图识别器"},
			{Role: domain.ContextRoleUser, Content: "推荐几款学生党手机"},
			{Role: domain.ContextRoleAssistant, Content: strings.Repeat("推荐了商品 12 和商品 18", 50)},
			{Role: domain.ContextRoleUser, Content: "把这个加入购物车"},
			{Role: domain.ContextRoleUser, Content: "加两件"},
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.ToolName != domain.ToolCartAdd {
		t.Fatalf("ToolName = %q, want %q", result.ToolName, domain.ToolCartAdd)
	}
	assertArguments(t, result.Arguments, map[string]any{"product_id": float64(12), "quantity": float64(2)})

	messages := fakeModel.lastMessages
	if len(messages) != 5 {
		t.Fatalf("LLM message count = %d, want prebuilt context", len(messages))
	}
	assertContextMessage(t, messages[0], domain.ContextRoleSystem, "意图识别器")
	assertContextMessage(t, messages[1], domain.ContextRoleUser, "推荐几款学生党手机")
	assertContextMessage(t, messages[2], domain.ContextRoleAssistant, strings.Repeat("推荐了商品 12 和商品 18", 50))
	assertContextMessage(t, messages[3], domain.ContextRoleUser, "把这个加入购物车")
	assertContextMessage(t, messages[4], domain.ContextRoleUser, "加两件")
}

func TestPlannerLLMForwardsPrebuiltToolMessage(t *testing.T) {
	planner, fakeModel := newPlannerAndFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"query","tool_name":"cart.list","arguments":{}}`},
	})

	_, err := planner.Plan(context.Background(), PlanRequest{
		Message: "看看购物车",
		Messages: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "意图识别器"},
			{Role: domain.ContextRoleTool, ToolName: "cart.list", ToolCallID: "call_001", Content: `{"items":[{"product_id":12}]}`},
			{Role: domain.ContextRoleUser, Content: "看看购物车"},
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	messages := fakeModel.lastMessages
	if len(messages) != 3 {
		t.Fatalf("LLM message count = %d, want system/tool/current", len(messages))
	}
	assertContextMessage(t, messages[1], domain.ContextRoleTool, "product_id")
	if messages[1].ToolName != "cart.list" {
		t.Fatalf("ToolName = %q, want cart.list", messages[1].ToolName)
	}
	if messages[1].ToolCallID != "call_001" {
		t.Fatalf("ToolCallID = %q, want call_001", messages[1].ToolCallID)
	}
}

func TestPlannerUsesContextForHighRiskActionButConfirmationComesFromRegistry(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"order.cancel","arguments":{"order_id":"202406300001"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{
		Message: "取消刚才那个订单",
		Messages: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "意图识别器"},
			{Role: domain.ContextRoleUser, Content: "查一下订单 202406300001"},
			{Role: domain.ContextRoleAssistant, Content: "订单 202406300001 当前待支付"},
			{Role: domain.ContextRoleUser, Content: "取消刚才那个订单"},
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.ToolName != domain.ToolOrderCancel {
		t.Fatalf("ToolName = %q, want %q", result.ToolName, domain.ToolOrderCancel)
	}
	if !result.RequireConfirmation {
		t.Fatal("RequireConfirmation = false, want true from registry metadata")
	}
}

func TestPlannerKeepsCurrentMessagePriorityOverContext(t *testing.T) {
	planner, fakeModel := newPlannerAndFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"query","tool_name":"order.get","arguments":{"order_id":"202406300002"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{
		Message: "查订单 202406300002",
		Messages: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "意图识别器"},
			{Role: domain.ContextRoleUser, Content: "查订单 202406300001"},
			{Role: domain.ContextRoleAssistant, Content: "订单 202406300001 当前待支付"},
			{Role: domain.ContextRoleUser, Content: "查订单 202406300002"},
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	assertArguments(t, result.Arguments, map[string]any{"order_id": "202406300002"})
	if got := fakeModel.lastMessages[len(fakeModel.lastMessages)-1].Content; got != "查订单 202406300002" {
		t.Fatalf("current message was not sent last: %q", got)
	}
}

func TestPlannerSanitizesSensitiveLLMArguments(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"cart.add","arguments":{"product_id":12,"quantity":1,"user_id":999,"token":"secret","session_id":"sid","auth":"bearer"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "把商品 12 加入购物车"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	for _, key := range []string{"user_id", "token", "session_id", "auth"} {
		if _, ok := result.Arguments[key]; ok {
			t.Fatalf("Arguments exposes sensitive key %q: %#v", key, result.Arguments)
		}
	}
}

func TestPlannerDoesNotExposeSensitiveContextValues(t *testing.T) {
	planner, fakeModel := newPlannerAndFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"chat","tool_name":"","arguments":{},"assistant_message":"好的"}`},
	})

	_, err := planner.Plan(context.Background(), PlanRequest{
		Message: "你好",
		Messages: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "意图识别器"},
			{Role: domain.ContextRoleUser, Content: "token=[redacted] user_id=[redacted] session_id=[redacted] auth=[redacted]"},
			{Role: domain.ContextRoleUser, Content: "你好"},
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	historyMessage := fakeModel.lastMessages[1].Content
	for _, leaked := range []string{"secret-token", "user_id=999", "session_id=abc", "auth=bearer"} {
		if strings.Contains(historyMessage, leaked) {
			t.Fatalf("history message leaked %q: %q", leaked, historyMessage)
		}
	}
}

func TestPlannerMissingContextReturnsQuestionWithoutGuessing(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"","arguments":{},"missing_params":["product_id"],"assistant_message":"请告诉我要加入购物车的商品 ID。"}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "加两件"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.ToolName != "" || len(result.MissingParams) != 1 || result.MissingParams[0] != "product_id" {
		t.Fatalf("Plan = %#v, want missing product_id without tool call", result)
	}
}

func TestPlannerCoreIntents(t *testing.T) {
	planner := New(tools.NewRegistry(config.ToolTimeoutConfig{}))

	tests := []struct {
		name                string
		message             string
		wantIntent          Intent
		wantTool            string
		wantRequireConfirm  bool
		wantArguments       map[string]any
		wantAssistantEmpty  bool
		wantMissingParamLen int
	}{
		{
			name:               "greeting is chat",
			message:            "你好",
			wantIntent:         IntentChat,
			wantAssistantEmpty: true,
		},
		{
			name:               "recommend product",
			message:            "推荐几款适合学生党的手机",
			wantIntent:         IntentRecommend,
			wantTool:           domain.ToolProductRecommend,
			wantArguments:      map[string]any{"query": "推荐几款适合学生党的手机"},
			wantAssistantEmpty: true,
		},
		{
			name:               "query order by id",
			message:            "查一下订单 202406300001",
			wantIntent:         IntentQuery,
			wantTool:           domain.ToolOrderGet,
			wantArguments:      map[string]any{"order_id": "202406300001"},
			wantAssistantEmpty: true,
		},
		{
			name:               "add product to cart",
			message:            "帮我加入购物车，商品 12 买 2 件",
			wantIntent:         IntentAction,
			wantTool:           domain.ToolCartAdd,
			wantArguments:      map[string]any{"product_id": int64(12), "quantity": int64(2)},
			wantAssistantEmpty: true,
		},
		{
			name:               "cancel order requires confirmation",
			message:            "取消订单 202406300001",
			wantIntent:         IntentAction,
			wantTool:           domain.ToolOrderCancel,
			wantRequireConfirm: true,
			wantArguments:      map[string]any{"order_id": "202406300001"},
			wantAssistantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := planner.Plan(context.Background(), PlanRequest{Message: tt.message})
			if err != nil {
				t.Fatalf("Plan returned error: %v", err)
			}

			if result.Intent != tt.wantIntent {
				t.Fatalf("Intent = %q, want %q", result.Intent, tt.wantIntent)
			}
			if result.ToolName != tt.wantTool {
				t.Fatalf("ToolName = %q, want %q", result.ToolName, tt.wantTool)
			}
			if result.RequireConfirmation != tt.wantRequireConfirm {
				t.Fatalf("RequireConfirmation = %v, want %v", result.RequireConfirmation, tt.wantRequireConfirm)
			}
			if len(result.MissingParams) != tt.wantMissingParamLen {
				t.Fatalf("len(MissingParams) = %d, want %d", len(result.MissingParams), tt.wantMissingParamLen)
			}
			if tt.wantAssistantEmpty && result.AssistantMessage != "" {
				t.Fatalf("AssistantMessage = %q, want empty", result.AssistantMessage)
			}
			assertArguments(t, result.Arguments, tt.wantArguments)
		})
	}
}

func TestPlannerMissingOrderID(t *testing.T) {
	planner := New(tools.NewRegistry(config.ToolTimeoutConfig{}))

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "帮我取消订单"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.Intent != IntentAction {
		t.Fatalf("Intent = %q, want %q", result.Intent, IntentAction)
	}
	if result.ToolName != "" {
		t.Fatalf("ToolName = %q, want empty", result.ToolName)
	}
	if result.RequireConfirmation {
		t.Fatal("RequireConfirmation = true, want false for missing params")
	}
	if len(result.MissingParams) != 1 || result.MissingParams[0] != "order_id" {
		t.Fatalf("MissingParams = %#v, want [order_id]", result.MissingParams)
	}
	if result.AssistantMessage == "" {
		t.Fatal("AssistantMessage should ask for order_id")
	}
}

func TestPlannerUsesRegistryConfirmationMetadata(t *testing.T) {
	registry := tools.NewRegistry(config.ToolTimeoutConfig{})
	planner := New(registry)

	result, err := planner.Plan(context.Background(), PlanRequest{Message: "取消订单 202406300001"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	metadata, err := registry.Metadata(domain.ToolOrderCancel)
	if err != nil {
		t.Fatalf("Metadata returned error: %v", err)
	}
	if result.RequireConfirmation != metadata.RequireConfirmation {
		t.Fatalf("RequireConfirmation = %v, want metadata value %v", result.RequireConfirmation, metadata.RequireConfirmation)
	}
}

func TestPlannerDoesNotExposeUserID(t *testing.T) {
	planner := New(tools.NewRegistry(config.ToolTimeoutConfig{}))

	for _, message := range []string{
		"推荐几款适合学生党的手机",
		"查一下订单 202406300001",
		"帮我加入购物车，商品 12 买 2 件",
		"取消订单 202406300001 user_id 999",
	} {
		t.Run(message, func(t *testing.T) {
			result, err := planner.Plan(context.Background(), PlanRequest{Message: message})
			if err != nil {
				t.Fatalf("Plan returned error: %v", err)
			}
			if _, ok := result.Arguments["user_id"]; ok {
				t.Fatalf("Arguments exposes user_id: %#v", result.Arguments)
			}
		})
	}
}

func assertArguments(t *testing.T, got, want map[string]any) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("Arguments = %#v, want %#v", got, want)
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Fatalf("Arguments missing key %q: %#v", key, got)
		}
		if gotValue != wantValue {
			t.Fatalf("Arguments[%q] = %#v, want %#v", key, gotValue, wantValue)
		}
	}
}

func newPlannerWithFakeLLM(t *testing.T, responses []fakeLLMResponse) *Planner {
	t.Helper()

	planner, _ := newPlannerAndFakeLLM(t, responses)
	return planner
}

func newPlannerAndFakeLLM(t *testing.T, responses []fakeLLMResponse) (*Planner, *fakeIntentModel) {
	t.Helper()

	fakeModel := &fakeIntentModel{responses: responses}
	factory := fakeIntentModelFactory{model: fakeModel}

	return New(
		tools.NewRegistry(config.ToolTimeoutConfig{}),
		WithIntentModel(factory, config.EinoConfig{Provider: "deepseek", Model: "fast-intent"}),
	), fakeModel
}

func assertContextMessage(t *testing.T, message domain.ContextMessage, role, contains string) {
	t.Helper()
	if message.Role != role {
		t.Fatalf("Role = %q, want %q", message.Role, role)
	}
	if !strings.Contains(message.Content, contains) {
		t.Fatalf("Content = %q, want to contain %q", message.Content, contains)
	}
}

type fakeLLMResponse struct {
	content string
	err     error
}

type fakeIntentModel struct {
	responses    []fakeLLMResponse
	calls        int
	lastMessages []domain.ContextMessage
}

func (m *fakeIntentModel) Generate(_ context.Context, messages []domain.ContextMessage) (string, error) {
	if m.calls >= len(m.responses) {
		return "", errors.New("unexpected Generate call")
	}
	m.lastMessages = messages
	response := m.responses[m.calls]
	m.calls++
	if response.err != nil {
		return "", response.err
	}
	return response.content, nil
}

type fakeIntentModelFactory struct {
	model IntentModel
	err   error
}

func (f fakeIntentModelFactory) NewIntentModel(context.Context, config.EinoConfig) (IntentModel, error) {
	return f.model, f.err
}
