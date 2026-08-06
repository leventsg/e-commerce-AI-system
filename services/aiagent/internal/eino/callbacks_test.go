package eino

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	einocallbacks "github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestAgentEventCallbackBridgeIgnoresNonSupervisorModelOutput(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "内部子 agent 回复"}}, nil)
	writer.Close()

	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: "product_agent"}, reader)
	bridge.onModelEnd(ctx, &einocallbacks.RunInfo{Name: "product_agent"}, &model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "内部子 agent 完整回复"}})

	if len(events) != 0 {
		t.Fatalf("events len = %d, want 0; events=%+v", len(events), events)
	}
}

func TestAgentEventCallbackBridgeStreamsModelDeltasAndBuffersFinalMessage(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](2)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "你"}}, nil)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "好"}}, nil)
	writer.Close()

	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2 deltas only", len(events))
	}
	if events[0].Type != domain.EventAssistantDelta || events[0].Content != "你" || events[0].Done {
		t.Fatalf("first event = %+v, want assistant_delta chunk", events[0])
	}
	if events[1].Type != domain.EventAssistantDelta || events[1].Content != "好" || events[1].Done {
		t.Fatalf("second event = %+v, want assistant_delta chunk", events[1])
	}
	if events[0].MessageID == "" {
		t.Fatal("assistant_delta message id should not be empty")
	}
	if events[1].MessageID != events[0].MessageID {
		t.Fatalf("assistant delta message ids = %q, %q; want same id", events[0].MessageID, events[1].MessageID)
	}
	final, ok := bridge.finalAssistantEvent()
	if !ok {
		t.Fatal("finalAssistantEvent returned ok=false, want buffered final message")
	}
	if final.Type != domain.EventAssistantMessage || final.Content != "你好" || !final.Done {
		t.Fatalf("final event = %+v, want assistant_message", final)
	}
	if final.MessageID != events[0].MessageID {
		t.Fatalf("final message id = %q, want delta message id %q", final.MessageID, events[0].MessageID)
	}
	if _, ok := bridge.finalAssistantEvent(); ok {
		t.Fatal("finalAssistantEvent should be idempotent and return ok=false after final is consumed")
	}
}

func TestAgentEventCallbackBridgeBuffersNonStreamingModelFinal(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	bridge.onModelEnd(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, &model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "完整回复"}})

	if len(events) != 0 {
		t.Fatalf("events len = %d, want no direct callback emission; events=%+v", len(events), events)
	}
	final, ok := bridge.finalAssistantEvent()
	if !ok {
		t.Fatal("finalAssistantEvent returned ok=false, want non-streaming final message")
	}
	if final.Type != domain.EventAssistantMessage || final.Content != "完整回复" || final.MessageID == "" || !final.Done {
		t.Fatalf("final event = %+v, want buffered assistant_message", final)
	}
}

func TestAgentEventCallbackBridgeSkipsToolCallModelChunks(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{
		Role:    schema.Assistant,
		Content: "我先帮您搜索。",
		ToolCalls: []schema.ToolCall{{
			ID: "call-1",
			Function: schema.FunctionCall{
				Name:      domain.ToolProductSearch,
				Arguments: `{"keyword":"无线耳机"}`,
			},
		}},
	}}, nil)
	writer.Close()

	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)

	if len(events) != 0 {
		t.Fatalf("events len = %d, want 0 for tool-call model chunks; events=%+v", len(events), events)
	}
}

func TestAgentEventCallbackBridgeEmitsToolProgressAndWrappedResult(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})
	info := &einocallbacks.RunInfo{Name: domain.ToolCheckoutPrepare}

	bridge.onToolStart(ctx, info, &einotool.CallbackInput{ArgumentsInJSON: `{"order_items":[{"sku_id":1,"quantity":1}]}`})
	bridge.onToolEnd(ctx, info, &einotool.CallbackOutput{Response: `{"pre_order_id":"pre-1","expire_time":1785910000}`})

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Type != domain.EventToolProgress || events[0].Tool != domain.ToolCheckoutPrepare || events[0].Content == "" || events[0].Done {
		t.Fatalf("progress event = %+v, want tool_progress", events[0])
	}
	if events[1].Type != domain.EventToolResult || events[1].Status != "success" || events[1].Tool != domain.ToolCheckoutPrepare {
		t.Fatalf("result event = %+v, want successful tool_result", events[1])
	}
	if events[1].Content != "预结算已创建，预订单号为 pre-1。" {
		t.Fatalf("result content = %q, want wrapped checkout summary", events[1].Content)
	}
	if events[0].MessageID == "" {
		t.Fatal("tool_progress message id should not be empty")
	}
	if events[1].MessageID != events[0].MessageID {
		t.Fatalf("tool progress/result message ids = %q, %q; want same id", events[0].MessageID, events[1].MessageID)
	}
}

func TestAgentEventCallbackBridgeDeduplicatesSameToolProgressAndResult(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})
	info := &einocallbacks.RunInfo{Name: domain.ToolProductSearch}
	args := &einotool.CallbackInput{ArgumentsInJSON: `{"keyword":"无线耳机","page":1}`}
	result := &einotool.CallbackOutput{Response: `{"products":[],"total":0}`}

	bridge.onToolStart(ctx, info, args)
	bridge.onToolStart(ctx, info, args)
	bridge.onToolEnd(ctx, info, result)
	bridge.onToolEnd(ctx, info, result)

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventToolProgress || events[1].Type != domain.EventToolResult {
		t.Fatalf("events=%+v, want progress then result", events)
	}
}

func TestAgentEventCallbackBridgeCoalescesSameToolProgressAndKeepsDistinctResults(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})
	info := &einocallbacks.RunInfo{Name: domain.ToolProductSearch}

	bridge.onToolStart(ctx, info, &einotool.CallbackInput{ArgumentsInJSON: `{"keyword":"无线耳机","page":1}`})
	bridge.onToolStart(ctx, info, &einotool.CallbackInput{ArgumentsInJSON: `{"keyword":"蓝牙耳机","page":1}`})
	bridge.onToolEnd(ctx, info, &einotool.CallbackOutput{Response: `{"products":[],"total":0}`})
	bridge.onToolEnd(ctx, info, &einotool.CallbackOutput{Response: `{"products":[{"id":1}],"total":1}`})

	if len(events) != 3 {
		t.Fatalf("events len = %d, want one progress and two distinct results; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventToolProgress {
		t.Fatalf("first event = %+v, want single coalesced tool_progress", events[0])
	}
	if events[1].Type != domain.EventToolResult || events[2].Type != domain.EventToolResult {
		t.Fatalf("events=%+v, want two distinct tool_result events", events)
	}
}

func TestAgentEventCallbackBridgeOnEventSkipsTransientEvents(t *testing.T) {
	ctx := context.Background()
	var hookEvents []domain.AgentEvent
	var emittedEvents []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{
		ConversationID: "conv-1",
		OnEvent: func(_ context.Context, event domain.AgentEvent) error {
			hookEvents = append(hookEvents, event)
			return nil
		},
	}, nil, func(_ context.Context, event domain.AgentEvent) error {
		emittedEvents = append(emittedEvents, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "你"}}, nil)
	writer.Close()
	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)
	bridge.onToolStart(ctx, &einocallbacks.RunInfo{Name: domain.ToolProductSearch}, &einotool.CallbackInput{ArgumentsInJSON: `{"keyword":"键盘"}`})

	if len(emittedEvents) != 2 {
		t.Fatalf("emitted events len = %d, want delta and progress only; events=%+v", len(emittedEvents), emittedEvents)
	}
	if len(hookEvents) != 0 {
		t.Fatalf("hook events len = %d, want no transient callback hook events; events=%+v", len(hookEvents), hookEvents)
	}
	final, ok := bridge.finalAssistantEvent()
	if !ok {
		t.Fatal("finalAssistantEvent returned ok=false, want buffered final assistant event")
	}
	if err := bridge.send(ctx, final); err != nil {
		t.Fatalf("send final assistant event: %v", err)
	}
	if len(hookEvents) != 1 || hookEvents[0].Type != domain.EventAssistantMessage {
		t.Fatalf("hook events=%+v, want only persistent final assistant event after explicit final send", hookEvents)
	}
}

func TestConsumeEventsEmitsBufferedAssistantFinalAfterIteratorEnd(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	req := RunRequest{ConversationID: "conv-1"}
	bridge := newAgentEventCallbackBridge(req, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](2)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "你"}}, nil)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "好"}}, nil)
	writer.Close()
	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)

	if len(events) != 2 {
		t.Fatalf("events len before iterator end = %d, want 2 deltas; events=%+v", len(events), events)
	}
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()
	(&agent{}).consumeEvents(ctx, iter, req, bridge, bridge.emit)

	if len(events) != 3 {
		t.Fatalf("events len after iterator end = %d, want 2 deltas and 1 final; events=%+v", len(events), events)
	}
	if events[2].Type != domain.EventAssistantMessage || events[2].Content != "你好" || !events[2].Done {
		t.Fatalf("final event = %+v, want buffered assistant_message", events[2])
	}
	if events[2].MessageID != events[0].MessageID {
		t.Fatalf("final message id = %q, want delta message id %q", events[2].MessageID, events[0].MessageID)
	}
}

func TestConsumeEventsSuppressesIteratorAssistantWhenBufferedFinalExists(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	req := RunRequest{ConversationID: "conv-1"}
	bridge := newAgentEventCallbackBridge(req, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})
	bridge.onModelEnd(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, &model.CallbackOutput{Message: schema.AssistantMessage("完整回复", nil)})

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		AgentName: supervisorAgentName,
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: schema.AssistantMessage("完整回复", nil),
			Role:    schema.Assistant,
		}},
	})
	gen.Close()
	(&agent{}).consumeEvents(ctx, iter, req, bridge, bridge.emit)

	if len(events) != 1 {
		t.Fatalf("events len = %d, want only one final assistant event; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantMessage || events[0].Content != "完整回复" {
		t.Fatalf("event = %+v, want buffered assistant_message", events[0])
	}
}
