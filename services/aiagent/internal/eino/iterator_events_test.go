package eino

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestConsumeEventsConvertsIteratorReasoningToThinkingDelta(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent(supervisorAgentName, &schema.Message{
			Role:             schema.Assistant,
			ReasoningContent: "我需要先分析用户想买什么。",
		}),
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantThinkingDelta || events[0].Content != "我需要先分析用户想买什么。" || events[0].Done {
		t.Fatalf("event = %+v, want assistant_thinking_delta", events[0])
	}
	if events[0].MessageID != "" {
		t.Fatalf("thinking message id = %q, want empty", events[0].MessageID)
	}
}

func TestConsumeEventsConvertsIteratorToolCallsToProgress(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent("product_agent", &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				ID: "call-1",
				Function: schema.FunctionCall{
					Name:      domain.ToolProductSearch,
					Arguments: `{"keyword":"无线耳机"}`,
				},
			}},
		}),
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventToolProgress || events[0].Tool != domain.ToolProductSearch || events[0].Status != "running" || events[0].Done {
		t.Fatalf("event = %+v, want tool_progress", events[0])
	}
	if !strings.Contains(events[0].DataJSON, "无线耳机") {
		t.Fatalf("data_json = %q, want tool arguments", events[0].DataJSON)
	}
}

func TestConsumeEventsMatchesRepeatedToolResultsByToolCallID(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent("product_agent", &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				{
					ID: "call-1",
					Function: schema.FunctionCall{
						Name:      domain.ToolProductSearch,
						Arguments: `{"keyword":"无线耳机"}`,
					},
				},
				{
					ID: "call-2",
					Function: schema.FunctionCall{
						Name:      domain.ToolProductSearch,
						Arguments: `{"keyword":"蓝牙耳机"}`,
					},
				},
			},
		}),
		toolIteratorEvent("product_agent", domain.ToolProductSearch, "call-2", `{"products":[{"id":2}],"total":1}`),
		toolIteratorEvent("product_agent", domain.ToolProductSearch, "call-1", `{"products":[],"total":0}`),
	})

	if len(events) != 4 {
		t.Fatalf("events len = %d, want 4; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventToolProgress || events[1].Type != domain.EventToolProgress || events[2].Type != domain.EventToolResult || events[3].Type != domain.EventToolResult {
		t.Fatalf("events = %+v, want progress, progress, result, result", events)
	}
	if events[0].MessageID == events[1].MessageID {
		t.Fatalf("progress ids should be distinct: %q", events[0].MessageID)
	}
	if events[2].MessageID != events[1].MessageID {
		t.Fatalf("call-2 result id = %q, want matching second progress id %q", events[2].MessageID, events[1].MessageID)
	}
	if events[3].MessageID != events[0].MessageID {
		t.Fatalf("call-1 result id = %q, want matching first progress id %q", events[3].MessageID, events[0].MessageID)
	}
}

func TestConsumeEventsConvertsIteratorToolMessageToResult(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		toolIteratorEvent("product_agent", domain.ToolProductSearch, "call-1", `{"products":[],"total":0}`),
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want tool_result only; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventToolResult {
		t.Fatalf("event = %+v, want tool_result", events[0])
	}
	if events[0].Tool != domain.ToolProductSearch || events[0].Status != "success" || events[0].Content != "找到 0 件商品。" {
		t.Fatalf("result event = %+v, want wrapped product result", events[0])
	}
	if events[0].DataJSON != `{"products":[],"total":0}` {
		t.Fatalf("data_json = %q, want raw object", events[0].DataJSON)
	}
}

func TestConsumeEventsKeepsFailedToolEnvelopeFailed(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		toolIteratorEvent("product_agent", domain.ToolProductSearch, "call-1", `{"status":"failed","tool_name":"product_search","attempt_count":3,"retry_count":2,"error":{"kind":"transient","code":"rpc_unavailable","message":"商品查询暂时不可用","retryable_in_current_run":false},"business_outcome":"not_executed"}`),
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want tool_result only; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventToolResult || events[0].Status != "failed" {
		t.Fatalf("event = %+v, want failed tool_result", events[0])
	}
	if events[0].BusinessExecuted {
		t.Fatalf("event = %+v, failed not_executed envelope must not be business executed", events[0])
	}
	if !strings.Contains(events[0].Content, "商品查询暂时不可用") {
		t.Fatalf("content = %q, want safe failure message", events[0].Content)
	}
}

func TestConsumeEventsEmitsRootFinalAssistantOnly(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent("product_agent", schema.AssistantMessage("内部子 agent 回复", nil)),
		assistantIteratorEvent(supervisorAgentName, schema.AssistantMessage("这是给用户的最终回复。", nil)),
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want only root final; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantMessage || events[0].Content != "这是给用户的最终回复。" || !events[0].Done {
		t.Fatalf("event = %+v, want root assistant_message", events[0])
	}
}

func TestConsumeEventsEmitsAssistantDeltaForStreamingContent(t *testing.T) {
	reader, writer := schema.Pipe[*schema.Message](2)
	writer.Send(&schema.Message{Role: schema.Assistant, Content: "你"}, nil)
	writer.Send(&schema.Message{Role: schema.Assistant, Content: "好"}, nil)
	writer.Close()

	events := consumeTestAgentEvents(t, []*adk.AgentEvent{{
		AgentName: supervisorAgentName,
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			IsStreaming:   true,
			MessageStream: reader,
			Role:          schema.Assistant,
		}},
	}})

	if len(events) != 3 {
		t.Fatalf("events len = %d, want two deltas and final assistant; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantDelta || events[0].Content != "你" || events[0].Done {
		t.Fatalf("first event = %+v, want assistant delta", events[0])
	}
	if events[1].Type != domain.EventAssistantDelta || events[1].Content != "好" || events[1].Done {
		t.Fatalf("second event = %+v, want assistant delta", events[1])
	}
	if events[2].Type != domain.EventAssistantMessage || events[2].Content != "你好" || !events[2].Done {
		t.Fatalf("final event = %+v, want final assistant from iterator stream", events[2])
	}
}

func TestConsumeEventsEmitsA2UIDataUpdatesForStreamingContent(t *testing.T) {
	reader, writer := schema.Pipe[*schema.Message](2)
	writer.Send(&schema.Message{Role: schema.Assistant, Content: "你"}, nil)
	writer.Send(&schema.Message{Role: schema.Assistant, Content: "好"}, nil)
	writer.Close()

	events := consumeRawTestAgentEvents(t, []*adk.AgentEvent{{
		AgentName: supervisorAgentName,
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			IsStreaming:   true,
			MessageStream: reader,
			Role:          schema.Assistant,
		}},
	}})

	var a2uiEvents []domain.AgentEvent
	for _, event := range events {
		if event.Type == domain.EventA2UI {
			a2uiEvents = append(a2uiEvents, event)
		}
	}
	if len(a2uiEvents) != 3 {
		t.Fatalf("a2ui events len = %d, want two chunks and done; events=%+v", len(a2uiEvents), a2uiEvents)
	}
	if !strings.Contains(a2uiEvents[0].DataJSON, `"component":"assistant_answer"`) || !strings.Contains(a2uiEvents[0].DataJSON, `"content":"你"`) {
		t.Fatalf("first a2ui event = %+v, want assistant chunk", a2uiEvents[0])
	}
	if !strings.Contains(a2uiEvents[2].DataJSON, `"mode":"done"`) {
		t.Fatalf("last a2ui event = %+v, want done patch", a2uiEvents[2])
	}
}

func TestConsumeEventsPreservesBusinessExecutedErrorDiagnostic(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent("cart_checkout_agent", &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				ID: "call-1",
				Function: schema.FunctionCall{
					Name:      domain.ToolCartAdd,
					Arguments: `{"sku_id":1,"quantity":1}`,
				},
			}},
		}),
		toolIteratorEvent("cart_checkout_agent", domain.ToolCartAdd, "call-1", `{"summary":"已加入购物车"}`),
		{Err: errors.New("model summary failed")},
	})

	if len(events) != 3 {
		t.Fatalf("events len = %d, want progress/result/error; events=%+v", len(events), events)
	}
	last := events[len(events)-1]
	if last.Type != domain.EventError || !last.BusinessExecuted || last.DataJSON != `{"business_executed":true}` {
		t.Fatalf("last event = %+v, want business executed error diagnostic", last)
	}
	if !strings.Contains(last.Content, "业务结果已产生") {
		t.Fatalf("error content = %q, want business-executed warning", last.Content)
	}
	if strings.Contains(last.Content, "model summary failed") {
		t.Fatalf("error content = %q, should not expose internal error detail", last.Content)
	}
}

func TestConsumeEventsFailsPendingA2UIToolOnIteratorError(t *testing.T) {
	events := consumeRawTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent("user_agent", &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				ID: "call-1",
				Function: schema.FunctionCall{
					Name:      domain.ToolAddressAdd,
					Arguments: `{"receiver_name":"张三"}`,
				},
			}},
		}),
		{Err: errors.New("checkpoint failed")},
	})

	var toolA2UI []domain.AgentEvent
	var errorEvent *domain.AgentEvent
	for i := range events {
		event := events[i]
		if event.Type == domain.EventA2UI && strings.Contains(event.DataJSON, `"component":"tool_call"`) {
			toolA2UI = append(toolA2UI, event)
		}
		if event.Type == domain.EventError {
			errorEvent = &event
		}
	}
	if len(toolA2UI) != 2 {
		t.Fatalf("tool a2ui events len = %d, want running and failed; events=%+v", len(toolA2UI), events)
	}
	if !strings.Contains(toolA2UI[0].DataJSON, `"status":"running"`) {
		t.Fatalf("first tool a2ui = %+v, want running", toolA2UI[0])
	}
	if !strings.Contains(toolA2UI[1].DataJSON, `"status":"failed"`) || !strings.Contains(toolA2UI[1].DataJSON, `"done":true`) {
		t.Fatalf("second tool a2ui = %+v, want failed done", toolA2UI[1])
	}
	if errorEvent == nil || strings.Contains(errorEvent.Content, "checkpoint failed") {
		t.Fatalf("error event = %+v, want sanitized frontend error", errorEvent)
	}
}

func TestConsumeEventsIgnoresAgentToolProgressAndResult(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{
		assistantIteratorEvent(supervisorAgentName, &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				ID: "agent-call-1",
				Function: schema.FunctionCall{
					Name:      "product_agent",
					Arguments: `{"task":"查商品"}`,
				},
			}},
		}),
		toolIteratorEvent(supervisorAgentName, "product_agent", "agent-call-1", "内部 agent 已完成"),
	})

	if len(events) != 0 {
		t.Fatalf("events len = %d, want agent tool events hidden; events=%+v", len(events), events)
	}
}

func TestConsumeEventsConvertsIteratorInterruptToConfirmationRequired(t *testing.T) {
	events := consumeTestAgentEvents(t, []*adk.AgentEvent{{
		Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{
			InterruptContexts: []*adk.InterruptCtx{{
				ID:          "interrupt-1",
				IsRootCause: true,
				Info: &ApprovalInfo{
					ConfirmationID: "confirm-1",
					ToolName:       domain.ToolOrderCancel,
					Action:         domain.ToolOrderCancel,
					Summary:        "确认取消订单？",
					ExpiresAt:      1785910000,
					ArgumentsSummary: map[string]any{
						"order_id": "order-1",
					},
				},
			}},
		}},
	}})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventConfirmationRequired || events[0].ConfirmationID != "confirm-1" || events[0].Tool != domain.ToolOrderCancel {
		t.Fatalf("event = %+v, want confirmation_required", events[0])
	}
	if events[0].Status != "pending" || !events[0].Done {
		t.Fatalf("event = %+v, want pending done confirmation", events[0])
	}
	if !strings.Contains(events[0].DataJSON, "interrupt-1") {
		t.Fatalf("data_json = %q, want interrupt id", events[0].DataJSON)
	}
}

func consumeTestAgentEvents(t *testing.T, inputEvents []*adk.AgentEvent) []domain.AgentEvent {
	t.Helper()
	events := consumeRawTestAgentEvents(t, inputEvents)
	filtered := make([]domain.AgentEvent, 0, len(events))
	for _, event := range events {
		if event.Type != domain.EventA2UI {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func consumeRawTestAgentEvents(t *testing.T, inputEvents []*adk.AgentEvent) []domain.AgentEvent {
	t.Helper()
	ctx := context.Background()
	var events []domain.AgentEvent
	req := RunRequest{ConversationID: "conv-1", MessageID: "run-1"}
	emit := func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	}
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	for _, event := range inputEvents {
		gen.Send(event)
	}
	gen.Close()
	(&agent{}).consumeEvents(ctx, iter, req, emit)
	return events
}

func assistantIteratorEvent(agentName string, message *schema.Message) *adk.AgentEvent {
	return &adk.AgentEvent{
		AgentName: agentName,
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: message,
			Role:    schema.Assistant,
		}},
	}
}

func toolIteratorEvent(agentName, toolName, toolCallID, content string) *adk.AgentEvent {
	return &adk.AgentEvent{
		AgentName: agentName,
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message:  schema.ToolMessage(content, toolCallID, schema.WithToolName(toolName)),
			Role:     schema.Tool,
			ToolName: toolName,
		}},
	}
}
