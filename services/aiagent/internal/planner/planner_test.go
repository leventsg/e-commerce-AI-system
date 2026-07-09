package planner

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
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

func TestPlannerLLMReceivesBoundedContextBeforeCurrentMessage(t *testing.T) {
	planner, fakeModel := newPlannerAndFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"cart.add","arguments":{"product_id":12,"quantity":2}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{
		Message: "加两件",
		History: []*aimessages.AiMessages{
			newHistoryMessage("m1", "user", "推荐几款学生党手机"),
			newHistoryMessage("m2", "assistant", "推荐了商品 12 和商品 18"),
			newHistoryMessage("m3", "user", "把这个加入购物车"),
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
	if len(messages) != 3 {
		t.Fatalf("LLM message count = %d, want system/context/current", len(messages))
	}
	if !strings.Contains(messages[1].Content, "最近上下文") || !strings.Contains(messages[1].Content, "商品 12") {
		t.Fatalf("context message does not contain bounded history summary: %q", messages[1].Content)
	}
	if messages[2].Content != "加两件" {
		t.Fatalf("last LLM message = %q, want current user message", messages[2].Content)
	}
}

func TestPlannerUsesContextForHighRiskActionButConfirmationComesFromRegistry(t *testing.T) {
	planner := newPlannerWithFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"action","tool_name":"order.cancel","arguments":{"order_id":"202406300001"}}`},
	})

	result, err := planner.Plan(context.Background(), PlanRequest{
		Message: "取消刚才那个订单",
		History: []*aimessages.AiMessages{
			newHistoryMessage("m1", "user", "查一下订单 202406300001"),
			newHistoryMessage("m2", "assistant", "订单 202406300001 当前待支付"),
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
		History: []*aimessages.AiMessages{
			newHistoryMessage("m1", "user", "查订单 202406300001"),
			newHistoryMessage("m2", "assistant", "订单 202406300001 当前待支付"),
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

func TestPlannerDoesNotExposeSensitiveHistoryInContextMessage(t *testing.T) {
	planner, fakeModel := newPlannerAndFakeLLM(t, []fakeLLMResponse{
		{content: `{"intent":"chat","tool_name":"","arguments":{},"assistant_message":"好的"}`},
	})

	_, err := planner.Plan(context.Background(), PlanRequest{
		Message: "你好",
		History: []*aimessages.AiMessages{
			newHistoryMessage("m1", "user", "token=secret-token user_id=999 session_id=abc auth=bearer"),
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	contextMessage := fakeModel.lastMessages[1].Content
	for _, leaked := range []string{"secret-token", "user_id=999", "session_id=abc", "auth=bearer"} {
		if strings.Contains(contextMessage, leaked) {
			t.Fatalf("context message leaked %q: %q", leaked, contextMessage)
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

func newPlannerAndFakeLLM(t *testing.T, responses []fakeLLMResponse) (*Planner, *fakeChatModel) {
	t.Helper()

	fakeModel := &fakeChatModel{responses: responses}
	factory := eino.NewModelFactory(eino.WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return fakeModel, nil
	}))

	return New(
		tools.NewRegistry(config.ToolTimeoutConfig{}),
		WithIntentModel(factory, config.EinoConfig{Provider: "deepseek", Model: "fast-intent"}),
	), fakeModel
}

func newHistoryMessage(id, role, content string) *aimessages.AiMessages {
	return &aimessages.AiMessages{
		Id:             id,
		ConversationId: "conv_test",
		UserId:         42,
		Role:           role,
		Content:        content,
		Metadata:       sql.NullString{},
		CreatedAt:      time.Now(),
	}
}

type fakeLLMResponse struct {
	content string
	err     error
}

type fakeChatModel struct {
	responses    []fakeLLMResponse
	calls        int
	lastMessages []*schema.Message
}

func (m *fakeChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if m.calls >= len(m.responses) {
		return nil, errors.New("unexpected Generate call")
	}
	m.lastMessages = messages
	response := m.responses[m.calls]
	m.calls++
	if response.err != nil {
		return nil, response.err
	}
	return schema.AssistantMessage(response.content, nil), nil
}

func (m *fakeChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}
