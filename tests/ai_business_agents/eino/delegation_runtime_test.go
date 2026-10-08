//go:build ai_business_agents

package eino

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

func TestDelegationRejectsLegacyBeforeBusinessExecution(t *testing.T) {
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("shopping_decision_agent", `{"request":"add product"}`), businessReply("cannot execute")},
		"shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2}`), businessReply("done")},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 0 {
		t.Fatalf("legacy delegation executed business RPC: %+v", *calls)
	}
}

func TestDelegationPreservesRealToolOutcomes(t *testing.T) {
	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{UserID: 7, ConversationID: "conv"})
	for _, tc := range []struct{ raw, want string }{
		{`{"status":"success","business_outcome":"not_executed","result":{"order_id":"O1"}}`, "success"},
		{`{"status":"failed","business_outcome":"not_executed"}`, "not_executed"},
		{`{"status":"failed","business_outcome":"unknown"}`, "unknown"},
		{`{"status":"failed","business_outcome":"executed"}`, "failed"},
	} {
		execution := &delegationExecution{}
		_, err := recordDelegationResult(ctx, execution, &adk.ToolContext{Name: "order_get", CallID: "call"}, []byte(`{}`), tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := execution.snapshot().Actions
		if len(got) != 1 || got[0].Status != tc.want {
			t.Fatalf("outcome %s: got %+v want %s", tc.raw, got, tc.want)
		}
	}
}

func TestDelegationReadOnlyBlocksHighRiskBeforeConfirmation(t *testing.T) {
	runner, recorder, confirmations, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName: {businessCall("after_sales_agent", `{"task":{"intent":"order_get","objective":"Inspect only"},"scope":{"mode":"read_only"}}`), businessReply("not cancelled")},
		"after_sales_agent": {businessCall("order_cancel", `{"order_id":"order_42"}`), businessReply(`{"status":"failed","summary":"blocked","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 0 || confirmations.created.ToolName != "" {
		t.Fatalf("read-only delegation reached write/confirmation: calls=%v confirmation=%+v", *calls, confirmations.created)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].Status != "failed" {
		t.Fatalf("guard denial missing audit trail: %v", recorder.calls)
	}
}

func TestDelegationExclusionBlocksLowRiskWrite(t *testing.T) {
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("shopping_decision_agent", `{"task":{"intent":"cart_add","objective":"Inspect cart"},"scope":{"mode":"execute","exclusions":["cart_add"]}}`), businessReply("not added")},
		"shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2}`), businessReply(`{"status":"failed","summary":"blocked","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 0 {
		t.Fatalf("excluded tool executed: %+v", *calls)
	}
}

func TestDelegationCannotGuessLookupGapOrChangeBoundInput(t *testing.T) {
	for _, request := range []string{
		`{"task":{"intent":"shopping_compare","objective":"Compare only"},"scope":{"mode":"execute"}}`,
		`{"task":{"intent":"cart_add","objective":"Add selected item"},"inputs":{"product_id":41,"quantity":2}}`,
		`{"task":{"intent":"cart_add","objective":"Add selected item"},"gaps":[{"field":"inputs.product_id","resolution":"lookup","blocks":"write"}]}`,
		`{"task":{"intent":"cart_add","objective":"Add selected item"},"gaps":[{"field":"inputs.quantity","resolution":"ask_user","blocks":"write"}]}`,
	} {
		runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
			supervisorAgentName:       {businessCall("shopping_decision_agent", request), businessReply("needs information")},
			"shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2}`), businessReply(`{"status":"completed","summary":"added","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
		})
		events, err := runner.Run(context.Background(), businessFlowRequest())
		if err != nil {
			t.Fatal(err)
		}
		assertNoBusinessFlowErrors(t, events)
		if len(*calls) != 0 {
			t.Fatalf("unproven/changed input reached RPC for %s: %v", request, *calls)
		}
	}
}

func TestDelegationReadsSameRunEvidenceWithoutStorage(t *testing.T) {
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName: {
			businessCall("product_discovery_agent", `{"task":{"intent":"product_search","objective":"Find product"}}`),
			businessCall("shopping_decision_agent", `{"task":{"intent":"cart_add","objective":"Add the unique result"},"inputs":{"quantity":2},"context":{"evidence":[{"ref":"tool_call:call_product_search"}]},"gaps":[{"field":"inputs.product_id","resolution":"lookup","blocks":"write"}]}`), businessReply("done"),
		},
		"product_discovery_agent": {businessCall("product_search", `{"keyword":"headphones"}`), businessReply(`{"status":"completed","summary":"found","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
		"shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2}`), businessReply(`{"status":"completed","summary":"added","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 2 || (*calls)[1].ToolName != "cart_add" {
		t.Fatalf("same-run evidence unavailable: %v", *calls)
	}
}

func TestDelegationScopeSurvivesConfirmationResume(t *testing.T) {
	runner, _, confirmations, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("order_fulfillment_agent", `{"task":{"intent":"order_create","objective":"Create selected order"},"scope":{"exclusions":["order_get"]}}`), businessReply("done")},
		"order_fulfillment_agent": {businessCall("order_create", `{"pre_order_id":"pre_42","address_id":1}`), businessCall("order_get", `{"order_id":"order_42"}`), businessReply(`{"status":"partial","summary":"created only","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
	})
	req := businessFlowRequest()
	events, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	pending := false
	for _, event := range events {
		if event.Type == domain.EventConfirmationRequired {
			pending = true
		}
	}
	if !pending || len(*calls) != 0 {
		t.Fatalf("expected pending approval before RPC: %v", events)
	}
	bound := confirmations.bound
	events, err = runner.Resume(context.Background(), ResumeRequest{UserID: 7, ConversationID: req.ConversationID, ConfirmationID: bound.ConfirmationID, RunID: bound.RunID, CheckpointID: bound.CheckpointID, InterruptID: bound.InterruptID, Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 1 || (*calls)[0].ToolName != "order_create" {
		t.Fatalf("scope lost on resume: %v", *calls)
	}
}

func TestDelegationDoesNotRepeatWriteAfterMalformedResult(t *testing.T) {
	request := `{"task":{"intent":"cart_add","objective":"Add selected item"},"inputs":{"product_id":42,"quantity":2}}`
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("shopping_decision_agent", request), businessCall("shopping_decision_agent", request), businessReply("review result")},
		"shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2}`), businessReply("malformed response"), businessCall("cart_add", `{"product_id":42,"quantity":2}`), businessReply(`{"status":"completed","summary":"added","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`)},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 1 {
		t.Fatalf("write executed %d times", len(*calls))
	}
}

func TestDelegationEquivalentNumbersCannotRepeatWrite(t *testing.T) {
	first := businessCall("cart_add", `{"product_id":1,"quantity":1}`)
	second := businessCall("cart_add", `{"product_id":1.0,"quantity":1e0}`)
	second.ToolCalls[0].ID = "second_cart_add"
	reply := `{"status":"completed","summary":"done","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`
	runner, _, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
		supervisorAgentName:       {businessCall("shopping_decision_agent", `{"task":{"intent":"cart_add","objective":"Add once"}}`), businessReply("done")},
		"shopping_decision_agent": {first, second, businessReply(reply)},
	})
	events, err := runner.Run(context.Background(), businessFlowRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNoBusinessFlowErrors(t, events)
	if len(*calls) != 1 {
		t.Fatalf("equivalent numeric arguments repeated write: %v", *calls)
	}
}

func TestDelegationReusedCallIDCannotExecuteOrOverwriteAudit(t *testing.T) {
	for _, invalidFirst := range []bool{true, false} {
		valid := businessCall("cart_add", `{"product_id":42,"quantity":2}`)
		invalid := businessCall("cart_add", `{"product_id":42,"unexpected":true}`)
		sequence := []*schema.Message{valid, invalid}
		if invalidFirst {
			sequence = []*schema.Message{invalid, valid}
		}
		sequence = append(sequence, businessChildReply("review actual results"))
		runner, recorder, _, calls := newBusinessFlowRunner(t, map[string][]*schema.Message{
			supervisorAgentName:       {businessCall("shopping_decision_agent", `{"task":{"intent":"cart_add","objective":"Add once"}}`), businessReply("done")},
			"shopping_decision_agent": sequence,
		})
		events, err := runner.Run(context.Background(), businessFlowRequest())
		if err != nil {
			t.Fatal(err)
		}
		assertNoBusinessFlowErrors(t, events)
		want := 1
		status := "success"
		if invalidFirst {
			want = 0
			status = "failed"
		}
		if len(*calls) != want || len(recorder.calls) != 1 || recorder.calls[0].Status != status {
			t.Fatalf("invalidFirst=%v calls=%v audit=%v", invalidFirst, *calls, recorder.calls)
		}
	}
}
