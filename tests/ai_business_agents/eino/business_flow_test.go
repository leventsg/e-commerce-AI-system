//go:build ai_business_agents

package eino

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

// These scripted models test actual ADK delegation and execution, not the
// semantic routing accuracy of a live model. Business RPCs are never called.
type businessFlowModel struct {
	script []*schema.Message
	next   int
}

func (m *businessFlowModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *businessFlowModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if m.next >= len(m.script) {
		return nil, fmt.Errorf("unexpected model call after script exhausted")
	}
	msg := m.script[m.next]
	m.next++
	return msg, nil
}
func (m *businessFlowModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}
func businessCall(name, args string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call_" + name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}}
}
func businessReply(content string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, Content: content}
}

// Child replies follow the production delegation result contract. Supervisor
// replies intentionally remain natural language.
func businessChildReply(summary string) *schema.Message {
    raw, err := json.Marshal(map[string]any{"status":"completed", "summary":summary, "evidence_refs":[]string{}, "actions":[]any{}, "missing_inputs":[]any{}, "remaining_work":[]string{}})
    if err != nil { panic(err) }
    return businessReply(string(raw))
}
func businessDelegation(intent, objective string) string {
    raw, err := json.Marshal(map[string]any{"task":map[string]string{"intent":intent,"objective":objective}})
    if err != nil { panic(err) }
    return string(raw)
}

type businessFlowRecorder struct{ calls []aitools.ToolCallRecord }

func (r *businessFlowRecorder) RecordToolCall(_ context.Context, record aitools.ToolCallRecord) error {
	r.calls = append(r.calls, record)
	return nil
}

// Persistence is stubbed here; status/ownership/expiry enforcement belongs to
// ConfirmationManager tests. This fixture exercises ADK interrupt/resume wiring.
type businessFlowConfirmations struct {
	created confirmation.CreateRequest
	bound   confirmation.ResumeTargetRequest
}

func (c *businessFlowConfirmations) Create(_ context.Context, req confirmation.CreateRequest) (*domain.Confirmation, error) {
	c.created = req
	return &domain.Confirmation{ID: "confirm_flow", UserID: req.UserID, ToolName: req.ToolName, Summary: req.Summary, ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (c *businessFlowConfirmations) BindResumeTarget(_ context.Context, req confirmation.ResumeTargetRequest) (*domain.Confirmation, error) {
	c.bound = req
	return &domain.Confirmation{ID: req.ConfirmationID}, nil
}

func newBusinessFlowRunner(t *testing.T, scripts map[string][]*schema.Message) (Runner, *businessFlowRecorder, *businessFlowConfirmations, *[]core.HandlerRequest) {
	t.Helper()
	calls := &[]core.HandlerRequest{}
	catalog := aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{})
	for i := range catalog {
		catalog[i].Handler = func(_ context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
			*calls = append(*calls, req)
			return core.HandlerResult{Data: map[string]any{"product_id": 42, "quantity": 2, "pre_order_id": "pre_42"}, Summary: "business result"}, nil
		}
		// Production confirmation summaries query RPCs. Isolate those reads while
		// retaining production metadata, schemas, guards and executor behavior.
		if catalog[i].Metadata.RequireConfirmation {
			catalog[i].ConfirmationSummary = func(_ context.Context, req core.ExecuteRequest) (string, error) {
				return "confirm " + req.ToolName, nil
			}
		}
	}
	registry := aitools.NewRegistry(catalog)
	recorder := &businessFlowRecorder{}
	executor := aitools.NewExecutor(registry, aitools.WithToolCallRecorder(recorder))
	confirmations := &businessFlowConfirmations{}
	// NewSupervisorAgent constructs business models in spec order, root last.
	names := []string{}
	for _, spec := range supervisorSubAgentSpecs {
		names = append(names, spec.name)
	}
	names = append(names, supervisorAgentName)
	built := 0
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		name := names[built]
		built++
		script := scripts[name]
		if len(script) == 0 {
			script = []*schema.Message{businessReply("unused agent")}
		}
		return &businessFlowModel{script: script}, nil
	}))
	runner, err := NewSupervisorAgent(context.Background(), factory, config.EinoConfig{Provider: "openai", Model: "scripted"}, registry, WithApprovalManager(aitools.NewApprovalManager(registry, executor, confirmations)))
	if err != nil {
		t.Fatal(err)
	}
	return runner, recorder, confirmations, calls
}
func businessFlowRequest() RunRequest {
	return RunRequest{UserID: 7, ConversationID: "conversation_flow", MessageID: "message_flow", Messages: []domain.ContextMessage{{Role: "user", Content: "Please complete the requested business flow"}}}
}
func assertNoBusinessFlowErrors(t *testing.T, events []domain.AgentEvent) {
	t.Helper()
	for _, event := range events {
		if event.Type == domain.EventError {
			t.Fatalf("runner error: %+v", event)
		}
	}
}

func TestBusinessAgentDelegationSequence(t *testing.T) {
	scripts := map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("product_discovery_agent", `{"task":{"intent":"product_search","objective":"Find headphones under 500"},"inputs":{"keyword":"headphones"}}`), businessCall("shopping_decision_agent", `{"task":{"intent":"cart_add","objective":"Selected product_id=42, quantity=2; add to cart"},"inputs":{"product_id":42,"quantity":2}}`), businessCall("order_fulfillment_agent", `{"task":{"intent":"checkout_prepare","objective":"product_id=42 quantity=2 already added; prepare checkout"},"inputs":{"order_items":[{"product_id":42,"quantity":2}]}}`), businessReply("final supervisor summary")},
		"product_discovery_agent": {businessCall("product_search", `{"keyword":"headphones"}`), businessChildReply("candidate product_id=42")},
		"shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2,"user_id":999}`), businessChildReply("cart addition complete")},
		"order_fulfillment_agent": {businessCall("checkout_prepare", `{"order_items":[{"product_id":42,"quantity":2}],"user_id":999}`), businessChildReply("checkout prepared")},
	}
	runner, recorder, _, calls := newBusinessFlowRunner(t, scripts)
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	got := []string{}
	for _, call := range *calls {
		got = append(got, call.ToolName)
		if call.UserID != 7 {
			t.Errorf("untrusted handler user ID: %d", call.UserID)
		}
		if _, ok := call.Arguments["user_id"]; ok {
			t.Error("model user_id reached handler arguments")
		}
	}
	if !slices.Equal(got, []string{"product_search", "cart_add", "checkout_prepare"}) {
		t.Fatalf("business calls = %v", got)
	}
	for _, name := range []string{"cart_add", "checkout_prepare"} {
		found := false
		for _, record := range recorder.calls {
			if record.ToolName == name && record.Status == "success" && record.Metadata.WriteOperation && record.UserID == 7 && record.ResultData != nil {
				found = true
			}
		}
		if !found {
			t.Errorf("missing write audit hook record for %s", name)
		}
	}
	final := 0
	for _, event := range events {
		if event.Type == domain.EventAssistantMessage {
			final++
			if event.Content != "final supervisor summary" {
				t.Errorf("internal reply leaked: %s", event.Content)
			}
		}
	}
	if final != 1 {
		t.Errorf("final assistant messages = %d", final)
	}
}

func TestBusinessAgentsHighRiskInterruptAndResume(t *testing.T) {
	cases := []struct{ agent, tool, args string }{
		{"order_fulfillment_agent", "order_create", `{"pre_order_id":"pre_42","address_id":1}`},
		{"after_sales_agent", "order_cancel", `{"order_id":"order_42"}`},
		{"shopping_decision_agent", "cart_delete", `{"cart_item_id":42}`},
	}
	for _, tc := range cases {
		for _, approved := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/approved=%v", tc.tool, approved), func(t *testing.T) {
				scripts := map[string][]*schema.Message{
					supervisorAgentName: {businessCall(tc.agent, businessDelegation(tc.tool, "perform requested action")), businessReply("supervisor completed")},
					tc.agent:            {businessCall(tc.tool, tc.args), businessChildReply("subagent completed")},
				}
				runner, recorder, confirmations, calls := newBusinessFlowRunner(t, scripts)
				req := businessFlowRequest()
				events, err := runner.Run(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				assertNoBusinessFlowErrors(t, events)
				pending := false
				for _, event := range events {
					if event.Type == domain.EventConfirmationRequired && event.Tool == tc.tool {
						pending = true
					}
				}
				if !pending {
					t.Fatalf("missing confirmation_required: %+v", events)
				}
				if len(*calls) != 0 {
					t.Fatalf("handler executed before approval: %+v", *calls)
				}
				bound := confirmations.bound
				if bound.InterruptID == "" || bound.CheckpointID == "" || confirmations.created.UserID != 7 {
					t.Fatalf("invalid confirmation target: %+v", bound)
				}
				events, err = runner.Resume(context.Background(), ResumeRequest{UserID: 7, ConversationID: req.ConversationID, ConfirmationID: bound.ConfirmationID, RunID: bound.RunID, CheckpointID: bound.CheckpointID, InterruptID: bound.InterruptID, Approved: approved})
				if err != nil {
					t.Fatal(err)
				}
				assertNoBusinessFlowErrors(t, events)
				want := 0
				if approved {
					want = 1
				}
				if len(*calls) != want {
					t.Fatalf("handler executions=%d want=%d", len(*calls), want)
				}
				if approved {
					if (*calls)[0].UserID != 7 || (*calls)[0].ToolName != tc.tool {
						t.Fatalf("invalid resumed execution: %+v", (*calls)[0])
					}
					recorded := false
					for _, record := range recorder.calls {
						if record.ToolName == tc.tool && record.Metadata.WriteOperation && record.Status == "success" {
							recorded = true
						}
					}
					if !recorded {
						t.Error("approved write did not reach audit hook")
					}
				} else {
					rejected := false
					for _, event := range events {
						if event.Type == domain.EventToolResult && event.Tool == tc.tool && event.Status == "rejected" {
							rejected = true
						}
						if event.BusinessExecuted {
							t.Error("rejected action marked executed")
						}
					}
					if !rejected {
						t.Error("missing rejected tool result")
					}
				}
			})
		}
	}
}

func TestBusinessAgentsDirectRequests(t *testing.T) {
	cases := []struct{ agent, tool, args string }{
		{"order_fulfillment_agent", "order_get", `{"order_id":"order_42"}`},
		{"order_fulfillment_agent", "address_update", `{"address_id":1,"recipient_name":"Lin","phone_number":"13800000000","province":"Zhejiang","city":"Hangzhou","detailed_address":"Example Street 1"}`},
		{"general_agent", "user_update", `{"user_name":"Lin"}`},
		{"general_agent", "", ""},
		{"after_sales_agent", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.agent+"/"+tc.tool, func(t *testing.T) {
			intent := tc.tool
			if intent == "" {
				intent = "general_consultation"
				if tc.agent == "after_sales_agent" { intent = "explain_policy" }
			}
			child := []*schema.Message{businessChildReply("response for supervisor")}
			if tc.tool != "" {
				child = append([]*schema.Message{businessCall(tc.tool, tc.args)}, child...)
			}
			runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
				supervisorAgentName: {businessCall(tc.agent, businessDelegation(intent, "direct request")), businessReply("final response")},
				tc.agent:            child,
			})
			events, err := runner.Run(context.Background(), businessFlowRequest())
			if err != nil {
				t.Fatal(err)
			}
			assertNoBusinessFlowErrors(t, events)
			if tc.tool == "" {
				if len(*calls) != 0 {
					t.Fatalf("explanation-only response executed business tools: %+v", *calls)
				}
			} else if len(*calls) != 1 || (*calls)[0].ToolName != tc.tool {
				t.Fatalf("unexpected business path: %+v", *calls)
			}
		})
	}
}

func TestBusinessAgentCannotExecuteAnotherAgentsWrite(t *testing.T) {
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("product_discovery_agent", `{"task":{"intent":"product_search","objective":"find product"}}`)},
		"product_discovery_agent": {businessCall("order_cancel", `{"order_id":"order_42"}`)},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unbound write reached handler: %+v", *calls)
	}
	for _, event := range events {
		if event.Type == domain.EventError {
			return
		}
	}
	t.Fatal("unbound write did not produce an error")
}

func TestBusinessAgentModelUnavailableDoesNotExecuteTools(t *testing.T) {
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return nil, errors.New("model offline")
	}))
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	if _, err := NewSupervisorAgent(context.Background(), factory, config.EinoConfig{Provider: "openai", Model: "offline"}, registry); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("factory error must support unavailable fallback: %v", err)
	}
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName: {businessCall("general_agent", `{"task":{"intent":"user_get","objective":"read profile"}}`)},
		"general_agent":     {businessChildReply("internal answer")},
	})
	// Root's script ends after delegation, simulating a failed final model call.
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatal("model failure caused a business operation")
	}
	for _, event := range events {
		if event.Type == domain.EventError && !event.BusinessExecuted {
			return
		}
	}
	t.Fatal("missing safe model failure event")
}

func TestBusinessWriteTimeoutRemainsFailed(t *testing.T) {
	catalog := aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{WriteSeconds: 1})
	registry := aitools.NewRegistry(catalog)
	recorder := &businessFlowRecorder{}
	executor := aitools.NewExecutor(registry, aitools.WithToolCallRecorder(recorder))
	event := executor.Execute(context.Background(), core.ExecuteRequest{
		UserID: 7, ConversationID: "conversation_flow", ToolName: "user_update", Arguments: map[string]any{"user_name": "Lin"},
	}, func(ctx context.Context, _ core.HandlerRequest) (core.HandlerResult, error) {
		<-ctx.Done()
		return core.HandlerResult{}, ctx.Err()
	})
	if event.Status != "failed" {
		t.Fatalf("timeout reported success: %+v", event)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].Status != "failed" || !recorder.calls[0].Metadata.WriteOperation {
		t.Fatalf("timeout not recorded as failed write: %+v", recorder.calls)
	}
}
