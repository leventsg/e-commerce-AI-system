//go:build ai_business_agents

package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

func TestDelegationSchemaContract(t *testing.T) {
	ctx := context.Background()
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	for _, spec := range supervisorSubAgentSpecs {
		t.Run(spec.name, func(t *testing.T) {
			params, err := buildDelegationSchema(ctx, spec, registry)
			if err != nil {
				t.Fatal(err)
			}
			js, err := params.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			if len(js.Required) != 1 || js.Required[0] != "task" {
				t.Fatalf("required=%v", js.Required)
			}
			if _, exists := js.Properties.Get("schema_version"); exists {
				t.Fatal("unexpected version")
			}
			for _, toolName := range spec.tools {
				raw := `{"task":{"intent":"` + toolName + `","objective":"complete requested task"}}`
				req, err := parseDelegationRequest(ctx, raw, spec, registry)
				if err != nil {
					t.Fatal(err)
				}
				meta, _ := registry.Metadata(toolName)
				want := "read_only"
				if meta.WriteOperation {
					want = "execute"
				}
				if req.Scope.Mode != want || len(req.Task.DoneWhen) == 0 {
					t.Fatalf("defaults=%+v", req)
				}
			}
		})
	}
}

func TestDelegationRejectsInvalidInput(t *testing.T) {
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	spec := supervisorSubAgentSpecs[3]
	for _, raw := range []string{
		`{}`, `{"request":"help"}`, `{"task":null}`, `{"task":{"intent":"order_get"}}`,
		`{"task":{"intent":"order_get","objective":""}}`,
		`{"task":{"intent":"cart_add","objective":"help"}}`,
		`{"task":{"intent":"order_get","objective":"help","unknown":1}}`,
		`{"task":{"intent":"order_get","objective":"help"},"schema_version":"1"}`,
		`{"task":{"intent":"order_get","objective":"help"},"inputs":{"user_id":12}}`,
		`{"task":{"intent":"order_get","objective":"help"},"inputs":{"order_id":123}}`,
		`{"task":{"intent":"order_get","objective":"help"},"inputs":{"order_id":null}}`,
		`{"task":{"intent":"order_get","objective":"help"},"scope":{"mode":"admin"}}`,
		`{"task":{"intent":"order_get","objective":"help"},"constraints":[{"rule":"keep","strength":"maybe"}]}`,
		`{"task":{"intent":"order_get","objective":"help"},"context":{"evidence":[{}]}}`,
		`{"task":{"intent":"order_get","objective":"help"},"context":{"prior_actions":[{"action":"bogus","tool_call_ref":"tool_call:x"}]}}`,
		`{"task":{"intent":"order_get","objective":"help"},"gaps":[{"field":"inputs.bogus","resolution":"lookup","blocks":"write"}]}`,
		`{"task":{"intent":"order_get","objective":"help"},"scope":{"exclusions":["cart_add"]}}`,
		`{"task":{"intent":"order_get","objective":"help"},"task":{"intent":"order_get","objective":"other"}}`,
	} {
		if _, err := parseDelegationRequest(context.Background(), raw, spec, registry); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{
		`{"task":{"intent":"order_get","objective":"help"},"inputs":{"order_id":null},"gaps":[{"field":"inputs.order_id","resolution":"lookup","blocks":"write"}]}`,
		`{"task":{"intent":"inspect_order_issue","objective":"help"},"context":{},"scope":{},"constraints":[],"gaps":[]}`,
	} {
		if _, err := parseDelegationRequest(context.Background(), raw, spec, registry); err != nil {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
	raw := `{"task":{"intent":"order_get","objective":"` + strings.Repeat("字", 2001) + `"}}`
	if _, err := parseDelegationRequest(context.Background(), raw, spec, registry); err == nil {
		t.Fatal("accepted oversized string")
	}
}

func TestDelegationToolArgumentsRequireCompleteBusinessParameters(t *testing.T) {
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	for _, args := range []map[string]any{{}, {"order_items": []any{map[string]any{"product_id": 1}}}, {"order_items": []any{map[string]any{"product_id": 1, "quantity": 1, "user_id": 9}}}} {
		if err := validateDelegationToolArguments(context.Background(), registry, "checkout_prepare", args); err == nil {
			t.Fatalf("accepted incomplete %v", args)
		}
	}
	if err := validateDelegationToolArguments(context.Background(), registry, "checkout_prepare", map[string]any{"order_items": []any{map[string]any{"product_id": json.Number("1e999999999"), "quantity": 1}}}); err == nil {
		t.Fatal("accepted unrepresentable numeric value")
	}
	args := map[string]any{"order_items": []any{map[string]any{"product_id": json.Number("9007199254740993"), "quantity": 2}}}
	if err := validateDelegationToolArguments(context.Background(), registry, "checkout_prepare", args); err != nil {
		t.Fatal(err)
	}
}

func TestDelegationResultContract(t *testing.T) {
	valid := `{"status":"completed","summary":"done","evidence_refs":[],"actions":[],"missing_inputs":[],"remaining_work":[]}`
	if _, err := parseDelegationResult(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"status":"completed"}`, strings.Replace(valid, `"completed"`, `"success"`, 1), strings.Replace(valid, `"actions":[]`, `"actions":[{"tool_name":"order_get","tool_call_ref":"tool_call:x","status":"completed"}]`, 1), strings.Replace(valid, `"summary":"done"`, `"summary":"done","invented":true`, 1)} {
		if _, err := parseDelegationResult(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestDelegationNumericEnumsAndExponentBounds(t *testing.T) {
	doc := delegationSchema{"type": "integer", "enum": []any{float64(1), float64(2)}}
	if err := validateDelegationValue(json.Number("1"), doc, "value"); err != nil {
		t.Fatal(err)
	}
	if err := validateDelegationValue(json.Number("3"), doc, "value"); err == nil {
		t.Fatal("accepted value outside numeric enum")
	}
	if err := validateDelegationValue(json.Number("1e-1000"), delegationSchema{"type": "number"}, "value"); err == nil {
		t.Fatal("accepted unsupported exponent")
	}
}

func TestDelegationLimitsAndNestedInputGaps(t *testing.T) {
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	spec := supervisorSubAgentSpecs[2]
	base := map[string]any{"task": map[string]any{"intent": "checkout_prepare", "objective": "prepare checkout"}}
	check := func(wantError bool) {
		t.Helper()
		raw, _ := json.Marshal(base)
		_, err := parseDelegationRequest(context.Background(), string(raw), spec, registry)
		if (err != nil) != wantError {
			t.Fatalf("err=%v wantError=%v", err, wantError)
		}
	}
	base["inputs"] = map[string]any{"order_items": []any{map[string]any{"product_id": 1, "quantity": nil}}}
	check(true)
	base["gaps"] = []any{map[string]any{"field": "inputs.order_items[].quantity", "resolution": "ask_user", "blocks": "write"}}
	check(false)
	delete(base, "gaps")
	base["inputs"] = map[string]any{"order_items": []any{map[string]any{"product_id": 1, "quantity": 2, "unknown": true}}}
	check(true)
	items := make([]any, 51)
	for i := range items {
		items[i] = map[string]any{"product_id": i + 1, "quantity": 1}
	}
	base["inputs"] = map[string]any{"order_items": items}
	check(true)
	base["inputs"] = map[string]any{"order_items": items[:50]}
	check(false)
	delete(base, "inputs")
	assumptions := make([]string, 21)
	for i := range assumptions {
		assumptions[i] = "unknown"
	}
	base["context"] = map[string]any{"assumptions": assumptions}
	check(true)
	base["context"] = map[string]any{"assumptions": assumptions[:20]}
	check(false)
	for i := range assumptions {
		assumptions[i] = strings.Repeat("x", 1900)
	}
	base["context"] = map[string]any{"assumptions": assumptions[:20]}
	check(true)
}
