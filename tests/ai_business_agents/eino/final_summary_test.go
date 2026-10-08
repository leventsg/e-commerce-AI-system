//go:build ai_business_agents

package eino

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

// exhaustedFlowModel 模拟"永不收敛"的模型：每轮都请求工具调用，
// 直到 finalSummaryMiddleware 在最后一次模型调用上注入总结指令并清空
// 工具列表。此时模型遵守约束输出文字总结，任务正常收尾。
type exhaustedFlowModel struct {
	agentName   string
	toolName    string
	modelCalls  int
	lastToolsN  int
	sawSummaryN bool
}

func (m *exhaustedFlowModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *exhaustedFlowModel) Generate(_ context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.modelCalls++
	common := model.GetCommonOptions(&model.Options{}, opts...)
	m.lastToolsN = len(common.Tools)
	last := msgs[len(msgs)-1]
	if last != nil && last.Role == schema.User && strings.Contains(last.Content, "工具调用轮数上限") {
		m.sawSummaryN = true
		return businessChildReply("已达到处理轮数上限：本轮无法继续调用工具，以上为已完成的执行结果，请补充需求后重试。"), nil
	}
	return &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			ID:       fmt.Sprintf("call_%s_%s_%d", m.agentName, m.toolName, m.modelCalls),
			Type:     "function",
			Function: schema.FunctionCall{Name: m.toolName, Arguments: `{}`},
		}},
	}, nil
}

func (m *exhaustedFlowModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// supervisor 模型：委派一次后总结。
type exhaustedSupervisorModel struct {
	calls int
}

func (m *exhaustedSupervisorModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *exhaustedSupervisorModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID:   "call_delegate_general",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "general_agent",
				Arguments: `{"task":{"intent":"user_get","objective":"read profile"}}`,
			},
		}}}, nil
	}
	return &schema.Message{Role: schema.Assistant, Content: "supervisor final summary"}, nil
}

func (m *exhaustedSupervisorModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// TestBusinessAgentIterationExhaustionProducesFinalSummary 验证迭代耗尽兜底：
// 子 agent 每轮都请求工具，MaxIterations 耗尽前的最后一次模型调用被
// finalSummaryMiddleware 改造为"无工具 + 总结指令"，最终以 assistant
// 消息收尾而不是 ErrExceedMaxIterations 错误。
func TestBusinessAgentIterationExhaustionProducesFinalSummary(t *testing.T) {
	calls := &[]core.HandlerRequest{}
	catalog := aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{})
	for i := range catalog {
		catalog[i].Handler = func(_ context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
			*calls = append(*calls, req)
			return core.HandlerResult{Data: map[string]any{"user_name": "Lin"}, Summary: "read ok"}, nil
		}
	}
	registry := aitools.NewRegistry(catalog)
	// NewExecutor 会调用 registry.setExecutor；缺少 executor 时所有工具调用
	// 都会被 tool.go 以 ErrToolHandlerRequired 拒绝。
	_ = aitools.NewExecutor(registry)

	generalModel := &exhaustedFlowModel{agentName: "general_agent", toolName: "user_get"}
	supervisorModel := &exhaustedSupervisorModel{}
	built := 0
	models := map[string]model.ToolCallingChatModel{}
	factory := NewModelFactory(WithChatModelBuilder(func(_ context.Context, _ string, _ config.EinoConfig) (model.BaseChatModel, error) {
		name := supervisorAgentName
		if built < len(supervisorSubAgentSpecs) {
			name = supervisorSubAgentSpecs[built].name
		}
		built++
		if name == supervisorAgentName {
			models[name] = supervisorModel
		} else if name == "general_agent" {
			models[name] = generalModel
		} else {
			models[name] = &exhaustedFlowModel{agentName: name, toolName: "user_get"}
		}
		return models[name], nil
	}))

	runner, err := NewSupervisorAgent(context.Background(), factory, config.EinoConfig{Provider: "openai", Model: "scripted"}, registry)
	if err != nil {
		t.Fatal(err)
	}
	events, err := runner.Run(context.Background(), RunRequest{
		UserID:         7,
		ConversationID: "conversation_exhausted",
		MessageID:      "message_exhausted",
		Messages:       []domain.ContextMessage{{Role: "user", Content: "please keep querying"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 兜底生效：没有 error 事件，正常以 supervisor 总结收尾。
	for _, event := range events {
		if event.Type == domain.EventError {
			t.Fatalf("iteration exhaustion surfaced as error: %+v", event)
		}
	}
	hasFinal := false
	for _, event := range events {
		if event.Type == domain.EventAssistantMessage && event.Content == "supervisor final summary" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Fatalf("missing supervisor final summary: %+v", events)
	}

	// 中间件确实在最后一次调用上注入了指令并清空了工具。
	if !generalModel.sawSummaryN {
		t.Fatal("final summary instruction was never injected")
	}
	if generalModel.lastToolsN != 0 {
		t.Fatalf("last model call still exposed %d tools, want 0", generalModel.lastToolsN)
	}
	// general_agent 恰好执行 MaxIterations 次模型调用（defaultAgentMaxIterations）。
	if generalModel.modelCalls != defaultAgentMaxIterations {
		t.Fatalf("general agent model calls = %d, want %d", generalModel.modelCalls, defaultAgentMaxIterations)
	}
	// 工具被执行了 MaxIterations-1 次：第 MaxIterations 次调用只输出总结。
	if len(*calls) != defaultAgentMaxIterations-1 {
		t.Fatalf("handler executions = %d, want %d", len(*calls), defaultAgentMaxIterations-1)
	}
}

// TestConsumeEventsEmitsFallbackAssistantWhenStreamEndsWithoutFinalMessage
// 验证事件层兜底：迭代器正常结束但只有工具结果、没有任何最终助手消息时，
// 补发一条 assistant_message 收尾，而不是让会话停留在工具事件上。
func TestConsumeEventsEmitsFallbackAssistantWhenStreamEndsWithoutFinalMessage(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent(supervisorAgentName, &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				ID: "call-1",
				Function: schema.FunctionCall{
					Name:      domain.ToolProductSearch,
					Arguments: `{"keyword":"无线耳机"}`,
				},
			}},
		}),
		toolIteratorEvent("product_discovery_agent", domain.ToolProductSearch, "call-1", `{"products":[],"total":0}`),
	})

	var fallback *domain.AgentEvent
	for i := range events {
		if events[i].Type == domain.EventAssistantMessage {
			fallback = &events[i]
		}
	}
	if fallback == nil {
		t.Fatalf("missing fallback assistant message: %+v", events)
	}
	if !fallback.Done {
		t.Fatalf("fallback event = %+v, want done", fallback)
	}
	if fallback.BusinessExecuted {
		t.Fatalf("read-only fallback must not be marked business executed: %+v", fallback)
	}
	if fallback.Content == "" {
		t.Fatalf("fallback content is empty: %+v", fallback)
	}
}

// TestConsumeEventsFallbackAssistantMarksBusinessExecuted 验证写操作已执行后
// 事件层兜底仍携带 business_executed 语义，提示用户不要重复操作。
func TestConsumeEventsFallbackAssistantMarksBusinessExecuted(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent("shopping_decision_agent", &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				ID: "call-1",
				Function: schema.FunctionCall{
					Name:      domain.ToolCartAdd,
					Arguments: `{"product_id":1,"quantity":1}`,
				},
			}},
		}),
		toolIteratorEvent("shopping_decision_agent", domain.ToolCartAdd, "call-1", `{"summary":"已加入购物车"}`),
	})

	var fallback *domain.AgentEvent
	for i := range events {
		if events[i].Type == domain.EventAssistantMessage {
			fallback = &events[i]
		}
	}
	if fallback == nil {
		t.Fatalf("missing fallback assistant message: %+v", events)
	}
	if !fallback.BusinessExecuted || fallback.DataJSON != `{"business_executed":true}` {
		t.Fatalf("fallback event = %+v, want business executed marker", fallback)
	}
}

// TestGraphRemainingIterationsWithoutState 验证非图上下文下安全放行。
func TestGraphRemainingIterationsWithoutState(t *testing.T) {
	if _, ok := graphRemainingIterations(context.Background()); ok {
		t.Fatal("plain context must not expose graph state")
	}
}
