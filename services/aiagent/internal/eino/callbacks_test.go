package eino

import (
	"context"
	"testing"
	"time"

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

func TestAgentEventCallbackBridgeStreamsModelDeltasWithoutBufferingFinalMessage(t *testing.T) {
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
}

func TestAgentEventCallbackBridgeEmitsModelDeltaBeforeStreamEOF(t *testing.T) {
	ctx := context.Background()
	events := make(chan domain.AgentEvent, 2)
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events <- event
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	done := make(chan struct{})
	go func() {
		bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)
		close(done)
	}()

	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "你"}}, nil)
	select {
	case event := <-events:
		if event.Type != domain.EventAssistantDelta || event.Content != "你" {
			t.Fatalf("event before EOF = %+v, want assistant_delta 你", event)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no assistant_delta emitted before stream EOF")
	}

	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "好"}}, nil)
	writer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("callback did not finish after stream close")
	}
}

func TestAgentEventCallbackBridgeStreamsReasoningAsThinkingDelta(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, ReasoningContent: "我需要先分析用户想买什么。"}}, nil)
	writer.Close()

	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1 thinking delta; events=%+v", len(events), events)
	}
	if events[0].Type != "assistant_thinking_delta" || events[0].Content != "我需要先分析用户想买什么。" || events[0].Done {
		t.Fatalf("event = %+v, want assistant_thinking_delta", events[0])
	}
	if events[0].MessageID != "" {
		t.Fatalf("thinking delta message id = %q, want empty", events[0].MessageID)
	}
}

func TestAgentEventCallbackBridgeStreamsContentBeforeLaterToolCall(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	bridge := newAgentEventCallbackBridge(RunRequest{ConversationID: "conv-1"}, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	reader, writer := schema.Pipe[*model.CallbackOutput](2)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "我先帮您搜索。"}}, nil)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			ID: "call-1",
			Function: schema.FunctionCall{
				Name:      domain.ToolProductSearch,
				Arguments: `{"keyword":"蓝牙耳机"}`,
			},
		}},
	}}, nil)
	writer.Close()

	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1 assistant delta; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantDelta || events[0].Content != "我先帮您搜索。" {
		t.Fatalf("event = %+v, want already streamed assistant delta", events[0])
	}
	if events[0].MessageID == "" {
		t.Fatal("assistant delta message id should not be empty")
	}
}

func TestAgentEventCallbackBridgeDoesNotBufferNonStreamingModelFinal(t *testing.T) {
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

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1 thinking delta for tool-call model chunks; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantThinkingDelta || events[0].Content != "我先帮您搜索。" {
		t.Fatalf("event = %+v, want tool-call content as thinking delta", events[0])
	}
	if events[0].MessageID != "" {
		t.Fatalf("thinking delta message id = %q, want empty", events[0].MessageID)
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

func TestAgentEventCallbackBridgeKeepsRepeatedToolCalls(t *testing.T) {
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

	if len(events) != 4 {
		t.Fatalf("events len = %d, want two progress/result pairs; events=%+v", len(events), events)
	}
	wantTypes := []string{domain.EventToolProgress, domain.EventToolProgress, domain.EventToolResult, domain.EventToolResult}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("event[%d] = %+v, want type %s; events=%+v", i, events[i], want, events)
		}
	}
	if events[0].MessageID == events[1].MessageID {
		t.Fatalf("progress message ids should be distinct for repeated tool calls: %q", events[0].MessageID)
	}
	if events[2].MessageID != events[0].MessageID || events[3].MessageID != events[1].MessageID {
		t.Fatalf("result ids = %q,%q want matching progress ids %q,%q", events[2].MessageID, events[3].MessageID, events[0].MessageID, events[1].MessageID)
	}
}

func TestAgentEventCallbackBridgeKeepsDistinctToolProgressAndResults(t *testing.T) {
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

	if len(events) != 4 {
		t.Fatalf("events len = %d, want two progress/result pairs; events=%+v", len(events), events)
	}
	wantTypes := []string{domain.EventToolProgress, domain.EventToolProgress, domain.EventToolResult, domain.EventToolResult}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("event[%d] = %+v, want type %s; events=%+v", i, events[i], want, events)
		}
	}
}

func TestConsumeEventsDoesNotSynthesizeAssistantFinalAfterIteratorEnd(t *testing.T) {
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

	if len(events) != 2 {
		t.Fatalf("events len after iterator end = %d, want only 2 deltas; events=%+v", len(events), events)
	}
}

func TestConsumeEventsEmitsIteratorAssistantAfterCallbackDelta(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	req := RunRequest{ConversationID: "conv-1"}
	bridge := newAgentEventCallbackBridge(req, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})
	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	writer.Send(&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "你"}}, nil)
	writer.Close()
	bridge.onModelEndWithStreamOutput(ctx, &einocallbacks.RunInfo{Name: supervisorAgentName}, reader)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		AgentName: supervisorAgentName,
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: schema.AssistantMessage("你好啊", nil),
			Role:    schema.Assistant,
		}},
	})
	gen.Close()
	(&agent{}).consumeEvents(ctx, iter, req, bridge, bridge.emit)

	if len(events) != 2 {
		t.Fatalf("events len = %d, want delta and iterator final assistant event; events=%+v", len(events), events)
	}
	if events[0].Type != domain.EventAssistantDelta || events[0].Content != "你" {
		t.Fatalf("first event = %+v, want callback assistant_delta", events[0])
	}
	if events[1].Type != domain.EventAssistantMessage || events[1].Content != "你好啊" {
		t.Fatalf("second event = %+v, want iterator assistant_message", events[1])
	}
	if events[1].MessageID == events[0].MessageID {
		t.Fatalf("iterator final message id = %q, want distinct from delta id %q", events[1].MessageID, events[0].MessageID)
	}
}

func TestConsumeEventsIgnoresIteratorToolMessages(t *testing.T) {
	ctx := context.Background()
	var events []domain.AgentEvent
	req := RunRequest{ConversationID: "conv-1"}
	bridge := newAgentEventCallbackBridge(req, nil, func(_ context.Context, event domain.AgentEvent) error {
		events = append(events, event)
		return nil
	})

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{
		AgentName: "product_agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: schema.ToolMessage(`{"page":1,"page_size":10,"products":[],"total":0}`, "call-1", schema.WithToolName(domain.ToolProductSearch)),
			Role:    schema.Tool,
		}},
	})
	gen.Close()
	(&agent{}).consumeEvents(ctx, iter, req, bridge, bridge.emit)

	if len(events) != 0 {
		t.Fatalf("events len = %d, want iterator tool message ignored; events=%+v", len(events), events)
	}
}
