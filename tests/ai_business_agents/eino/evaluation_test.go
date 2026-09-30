//go:build ai_business_agents

package eino

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/evaluation"
	tools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

const evaluationFixture = `{"products":{"42":{"product_id":42,"name":"headphones","price":10000}},"inventory":{"42":10},"cart":{"8":{"cart_item_id":8,"product_id":42,"quantity":2}},"addresses":{"1":{"address_id":1,"user_id":7,"city":"Synthetic"}},"orders":{"order_1":{"order_id":"order_1","user_id":7,"status":"pending_payment"}}}`

func scriptedEvaluation(t *testing.T, scripts map[string][]*schema.Message, failures []EvaluationFailure) *EvaluationTrial {
	t.Helper()
	names := []string{}
	for _, s := range supervisorSubAgentSpecs {
		names = append(names, s.name)
	}
	names = append(names, supervisorAgentName)
	built := 0
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		name := names[built]
		built++
		script := scripts[name]
		if len(script) == 0 {
			script = []*schema.Message{businessReply("unused")}
		}
		return &businessFlowModel{script: script}, nil
	}))
	trial, err := NewEvaluationTrial(context.Background(), EvaluationRequest{TrialID: "test", Fixture: json.RawMessage(evaluationFixture), Failures: failures}, config.EinoConfig{Provider: "openai", Model: "script"}, factory, nil)
	if err != nil {
		t.Fatal(err)
	}
	return trial
}
func TestEvaluationConfirmationSecurity(t *testing.T) {
	for _, mode := range []string{"approve", "reject", "expired", "cross_user"} {
		t.Run(mode, func(t *testing.T) {
			trial := scriptedEvaluation(t, map[string][]*schema.Message{supervisorAgentName: {businessCall("shopping_decision_agent", `{"request":"delete cart item 8"}`), businessReply("finished")}, "shopping_decision_agent": {businessCall("cart_delete", `{"cart_item_id":8}`), businessReply("deleted")}}, nil)
			response := trial.Execute(context.Background(), EvaluationRequest{Op: "chat", TrialID: "test", Content: "delete cart item 8"})
			if response.Error != "" {
				t.Fatal(response.Error)
			}
			id := ""
			for _, e := range response.Events {
				if e["type"] == "confirmation_required" {
					id, _ = e["confirmation_id"].(string)
				}
			}
			if id == "" {
				t.Fatalf("missing confirmation: %+v", response.Events)
			}
			if len(response.State.Cart) != 1 {
				t.Fatal("write before approval")
			}
			req := EvaluationRequest{Op: "confirm", TrialID: "test", ConfirmationID: id, Approved: true}
			switch mode {
			case "reject":
				req.Approved = false
			case "expired":
				req.AdvanceSeconds = 301
			case "cross_user":
				req.UserID = 8
			}
			response = trial.Execute(context.Background(), req)
			if mode == "approve" {
				if len(response.State.Cart) != 0 || len(response.State.Audit) != 1 {
					t.Fatalf("missing mutation/audit: %+v", response.State)
				}
				again := trial.Execute(context.Background(), req)
				if again.Error == "" {
					t.Fatal("duplicate accepted")
				}
			} else if len(response.State.Cart) != 1 {
				t.Fatal("forbidden mutation")
			}
		})
	}
}
func TestEvaluationActualParametersAndFailureInjection(t *testing.T) {
	for _, kind := range []string{"business_failure", "outcome_unknown", "audit_failure"} {
		t.Run(kind, func(t *testing.T) {
			trial := scriptedEvaluation(t, map[string][]*schema.Message{supervisorAgentName: {businessCall("shopping_decision_agent", `{"request":"add product 42 quantity 2"}`), businessReply("review outcome")}, "shopping_decision_agent": {businessCall("cart_add", `{"product_id":42,"quantity":2,"user_id":999}`), businessReply("review outcome")}}, []EvaluationFailure{{Tool: "cart_add", Call: 1, Kind: kind}})
			result := trial.Execute(context.Background(), EvaluationRequest{Op: "chat", TrialID: "test", Content: "add two product 42"})
			if result.Error != "" {
				t.Fatal(result.Error)
			}
			q := result.State.Cart["8"]["quantity"].(float64)
			if kind == "business_failure" && q != 2 {
				t.Fatal("failed operation mutated state")
			}
			if kind != "business_failure" && q != 4 {
				t.Fatal("missing injected side effect")
			}
			for _, e := range result.Trajectory {
				if e.Kind == "handler" && e.UserID != 0 && e.UserID != 7 {
					t.Fatal("untrusted user propagated")
				}
			}
		})
	}
}
func TestEvaluationSchemaAndIsolation(t *testing.T) {
	schemas := EvaluationSchema()["schemas"].(map[string]any)
	if len(schemas) < 27 {
		t.Fatal("incomplete catalog")
	}
	s, _ := evaluation.NewState(json.RawMessage(evaluationFixture))
	other, _ := evaluation.NewState(json.RawMessage(evaluationFixture))
	_, err := s.Apply("cart_add", map[string]any{"product_id": float64(42), "quantity": float64(3)}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if other.Cart["8"]["quantity"] != float64(2) {
		t.Fatal("trial state leaked")
	}
	for _, def := range tools.DefaultBusinessTools(tools.DefaultToolClients{}, config.ToolTimeoutConfig{}) {
		if def.Name == "cart_add" {
			trial := scriptedEvaluation(t, nil, nil)
			_, err = trial.handler(def)(context.Background(), core.HandlerRequest{UserID: 7, ToolName: "cart_add", Arguments: map[string]any{"product_id": "42", "quantity": float64(1)}})
			if err == nil {
				t.Fatal("wrong parameter type accepted")
			}
		}
	}
}
func TestEvaluationCheckoutReservesAndCannotRepeatOrder(t *testing.T) {
	state, _ := evaluation.NewState(json.RawMessage(evaluationFixture))
	prepared, err := state.Apply("checkout_prepare", map[string]any{"order_items": []any{map[string]any{"product_id": float64(42), "quantity": float64(2)}}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if state.Inventory["42"] != 8 {
		t.Fatal("checkout did not reserve inventory")
	}
	args := map[string]any{"pre_order_id": prepared["pre_order_id"], "address_id": float64(1)}
	if _, err = state.Apply("order_create", args, 7); err != nil {
		t.Fatal(err)
	}
	if _, err = state.Apply("order_create", args, 7); err == nil {
		t.Fatal("checkout consumed twice")
	}
}
