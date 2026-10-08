package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

// DelegationRequest carries model-provided task data, never authentication or
// approval authority. Inputs are checked against schemas from the tool registry
// so changes to business parameter types cannot silently diverge here.
type DelegationRequest struct {
	Task        DelegationTask         `json:"task"`
	Inputs      map[string]any         `json:"inputs,omitempty"`
	Constraints []DelegationConstraint `json:"constraints,omitempty"`
	Context     DelegationContext      `json:"context,omitempty"`
	Gaps        []DelegationGap        `json:"gaps,omitempty"`
	Scope       DelegationScope        `json:"scope,omitempty"`
}
type DelegationTask struct {
	Intent    string   `json:"intent"`
	Objective string   `json:"objective"`
	DoneWhen  []string `json:"done_when,omitempty"`
}
type DelegationConstraint struct {
	Rule       string   `json:"rule"`
	Strength   string   `json:"strength"`
	SourceRefs []string `json:"source_refs,omitempty"`
}
type DelegationContext struct {
	Evidence     []DelegationEvidence    `json:"evidence,omitempty"`
	PriorActions []DelegationPriorAction `json:"prior_actions,omitempty"`
	Assumptions  []string                `json:"assumptions,omitempty"`
}
type DelegationEvidence struct {
	Ref     string `json:"ref"`
	Purpose string `json:"purpose,omitempty"`
}
type DelegationPriorAction struct {
	Action      string `json:"action"`
	ToolCallRef string `json:"tool_call_ref"`
}
type DelegationGap struct {
	Field      string `json:"field"`
	Reason     string `json:"reason,omitempty"`
	Resolution string `json:"resolution"`
	Blocks     string `json:"blocks"`
}
type DelegationScope struct {
	Mode       string   `json:"mode,omitempty"`
	Exclusions []string `json:"exclusions,omitempty"`
}
type DelegationResult struct {
	Status        string                   `json:"status"`
	Summary       string                   `json:"summary"`
	EvidenceRefs  []string                 `json:"evidence_refs"`
	Actions       []DelegationAction       `json:"actions"`
	MissingInputs []DelegationMissingInput `json:"missing_inputs"`
	RemainingWork []string                 `json:"remaining_work"`
}
type DelegationAction struct {
	ToolName    string `json:"tool_name"`
	ToolCallRef string `json:"tool_call_ref"`
	Status      string `json:"status"`
}
type DelegationMissingInput struct {
	Field      string `json:"field"`
	Reason     string `json:"reason"`
	Resolution string `json:"resolution"`
}

// These schema documents only use the bounded subset validated below. They are
// marshalled into Eino's schema at the model boundary; no Eino type escapes.
type delegationSchema map[string]any

func delegationString(values ...string) delegationSchema {
	s := delegationSchema{"type": "string", "minLength": 1, "maxLength": 2000}
	if len(values) > 0 {
		s["enum"] = values
	}
	return s
}
func delegationObject(properties map[string]any, required ...string) delegationSchema {
	s := delegationSchema{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func delegationArray(items any, limit int) delegationSchema {
	return delegationSchema{"type": "array", "items": items, "maxItems": limit}
}

func delegationExtraIntents(name string) []string {
	switch name {
	case "shopping_decision_agent":
		return []string{"shopping_compare"}
	case "after_sales_agent":
		return []string{"inspect_order_issue", "explain_policy"}
	case "general_agent":
		return []string{"general_consultation", "explain_policy"}
	default:
		return nil
	}
}

func delegationToolSchema(ctx context.Context, registry *aitools.Registry, name string) (delegationSchema, error) {
	tool, err := registry.Tool(name)
	if err != nil {
		return nil, err
	}
	info, err := tool.Info(ctx)
	if err != nil {
		return nil, err
	}
	if info.ParamsOneOf == nil {
		return delegationObject(map[string]any{}), nil
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(js)
	if err != nil {
		return nil, err
	}
	var result delegationSchema
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func delegationBusinessNames() []string {
	seen := map[string]bool{}
	var result []string
	for _, spec := range supervisorSubAgentSpecs {
		for _, name := range spec.tools {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	sort.Strings(result)
	return result
}

func delegationInputSchema(ctx context.Context, spec agentSpec, registry *aitools.Registry) (delegationSchema, error) {
	properties := map[string]any{}
	for _, name := range spec.tools {
		source, err := delegationToolSchema(ctx, registry, name)
		if err != nil {
			return nil, err
		}
		fields, _ := source["properties"].(map[string]any)
		for key, value := range fields {
			if key == "user_id" {
				continue
			}
			relaxed := relaxDelegationInput(value)
			if prior, ok := properties[key]; ok && !reflect.DeepEqual(prior, relaxed) {
				// Some business tools share a field name with different types (for example
				// filters). Preserve each branch; execution validates the selected tool.
				properties[key] = delegationSchema{"anyOf": []any{prior, relaxed}}
			} else {
				properties[key] = relaxed
			}
		}
	}
	extras := map[string]any{}
	switch spec.name {
	case "product_discovery_agent":
		extras["query"] = delegationString()
		extras["product_ids"] = delegationArray(delegationSchema{"type": "integer"}, 50)
	case "shopping_decision_agent":
		extras["scenario"] = delegationString()
		extras["budget"] = delegationSchema{"type": "number"}
		extras["currency"] = delegationString()
		extras["candidate_product_ids"] = delegationArray(delegationSchema{"type": "integer"}, 50)
	case "after_sales_agent":
		extras["issue"] = delegationString()
	case "general_agent":
		extras["topic"] = delegationString()
	}
	for key, value := range extras {
		if _, ok := properties[key]; !ok {
			properties[key] = relaxDelegationInput(value)
		}
	}
	return delegationObject(properties), nil
}

// Delegation may carry partial business data. Unlike an execution schema,
// nested fields are optional and null is an explicit gap, checked after parse.
func relaxDelegationInput(value any) any {
	source := asDelegationSchema(value)
	copy := delegationSchema{}
	for key, value := range source {
		copy[key] = value
	}
	delete(copy, "required")
	switch copy["type"] {
	case "object":
		fields := map[string]any{}
		for key, value := range asDelegationSchema(copy["properties"]) {
			if key != "user_id" {
				fields[key] = relaxDelegationInput(value)
			}
		}
		copy["properties"] = fields
		copy["additionalProperties"] = false
	case "array":
		copy["maxItems"] = 50
		copy["items"] = relaxDelegationInput(copy["items"])
	case "string":
		copy["maxLength"] = 2000
	}
	return delegationSchema{"anyOf": []any{copy, delegationSchema{"type": "null"}}}
}

func buildDelegationSchema(ctx context.Context, spec agentSpec, registry *aitools.Registry) (*schema.ParamsOneOf, error) {
	doc, err := delegationRequestSchema(ctx, spec, registry)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var js jsonschema.Schema
	if err = json.Unmarshal(data, &js); err != nil {
		return nil, err
	}
	return schema.NewParamsOneOfByJSONSchema(&js), nil
}

func delegationRequestSchema(ctx context.Context, spec agentSpec, registry *aitools.Registry) (delegationSchema, error) {
	inputs, err := delegationInputSchema(ctx, spec, registry)
	if err != nil {
		return nil, err
	}
	intents := append(append([]string{}, spec.tools...), delegationExtraIntents(spec.name)...)
	var paths []string
	delegationInputPaths(inputs, "inputs", &paths)
	sort.Strings(paths)
	paths = slices.Compact(paths)
	registered := []string{}
	for _, name := range delegationBusinessNames() {
		if _, err := registry.Metadata(name); err == nil {
			registered = append(registered, name)
		}
	}
	refs := delegationArray(delegationString(), 20)
	return delegationObject(map[string]any{
		"task":        delegationObject(map[string]any{"intent": delegationString(intents...), "objective": delegationString(), "done_when": delegationArray(delegationString(), 20)}, "intent", "objective"),
		"inputs":      inputs,
		"constraints": delegationArray(delegationObject(map[string]any{"rule": delegationString(), "strength": delegationString("must", "prefer"), "source_refs": refs}, "rule", "strength"), 20),
		"context": delegationObject(map[string]any{
			"evidence":      delegationArray(delegationObject(map[string]any{"ref": delegationString(), "purpose": delegationString()}, "ref"), 20),
			"prior_actions": delegationArray(delegationObject(map[string]any{"action": delegationString(registered...), "tool_call_ref": delegationString()}, "action", "tool_call_ref"), 20),
			"assumptions":   delegationArray(delegationString(), 20),
		}),
		"gaps":  delegationArray(delegationObject(map[string]any{"field": delegationString(paths...), "reason": delegationString(), "resolution": delegationString("lookup", "ask_user", "return_to_parent"), "blocks": delegationString("task", "write")}, "field", "resolution", "blocks"), 20),
		"scope": delegationObject(map[string]any{"mode": delegationString("read_only", "prepare", "execute"), "exclusions": delegationArray(delegationString(spec.tools...), 20)}),
	}, "task"), nil
}

func delegationInputPaths(value any, path string, result *[]string) {
	s := asDelegationSchema(value)
	if branches, ok := s["anyOf"].([]any); ok {
		for _, branch := range branches {
			delegationInputPaths(branch, path, result)
		}
	}
	for key, child := range asDelegationSchema(s["properties"]) {
		next := path + "." + key
		*result = append(*result, next)
		delegationInputPaths(child, next, result)
	}
	if items, ok := s["items"]; ok {
		delegationInputPaths(items, path+"[]", result)
	}
}

func parseDelegationRequest(ctx context.Context, raw string, spec agentSpec, registry *aitools.Registry) (*DelegationRequest, error) {
	doc, err := delegationRequestSchema(ctx, spec, registry)
	if err != nil {
		return nil, err
	}
	value, err := decodeDelegationJSON(raw)
	if err != nil {
		return nil, err
	}
	if err = validateDelegationValue(value, doc, "$"); err != nil {
		return nil, err
	}
	var req DelegationRequest
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&req); err != nil {
		return nil, err
	}
	gapFields := map[string]bool{}
	for _, gap := range req.Gaps {
		gapFields[gap.Field] = true
	}
	if err = validateDelegationNulls(req.Inputs, "inputs", gapFields); err != nil {
		return nil, err
	}
	for _, evidence := range req.Context.Evidence {
		if !validDelegationRef(evidence.Ref, false) {
			return nil, fmt.Errorf("context.evidence.ref: invalid reference")
		}
	}
	for _, action := range req.Context.PriorActions {
		if !validDelegationRef(action.ToolCallRef, true) {
			return nil, fmt.Errorf("context.prior_actions.tool_call_ref: invalid reference")
		}
	}
	for _, constraint := range req.Constraints {
		for _, ref := range constraint.SourceRefs {
			if !validDelegationRef(ref, false) {
				return nil, fmt.Errorf("constraints.source_refs: invalid reference")
			}
		}
	}
	if req.Scope.Mode == "" {
		req.Scope.Mode = "read_only"
		if meta, err := registry.Metadata(req.Task.Intent); err == nil && meta.WriteOperation {
			req.Scope.Mode = "execute"
		}
	}
	if len(req.Task.DoneWhen) == 0 {
		req.Task.DoneWhen = []string{"Complete the requested " + req.Task.Intent + " task using verified evidence; return missing information or a truthful failure when completion is not possible."}
	}
	return &req, nil
}

func validDelegationRef(ref string, toolOnly bool) bool {
	kind, id, ok := strings.Cut(ref, ":")
	return ok && strings.TrimSpace(id) != "" && (kind == "tool_call" || (!toolOnly && kind == "message"))
}

func validateDelegationNulls(value any, path string, gaps map[string]bool) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if err := validateDelegationNulls(child, path+"."+key, gaps); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateDelegationNulls(child, path+"[]", gaps); err != nil {
				return err
			}
		}
	case nil:
		if !gaps[path] {
			return fmt.Errorf("%s: null requires an explicit gap", path)
		}
	}
	return nil
}

func validateDelegationToolArguments(ctx context.Context, registry *aitools.Registry, toolName string, args map[string]any) error {
	doc, err := delegationToolSchema(ctx, registry, toolName)
	if err != nil {
		return err
	}
	strictDelegationExecutionSchema(doc)
	// Normalize callers' concrete numeric types without losing integer precision.
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	value, err := decodeDelegationJSON(string(raw))
	if err != nil {
		return err
	}
	return validateDelegationValue(value, doc, "arguments")
}

func strictDelegationExecutionSchema(value any) {
	s := asDelegationSchema(value)
	switch s["type"] {
	case "object":
		s["additionalProperties"] = false
		for _, v := range asDelegationSchema(s["properties"]) {
			strictDelegationExecutionSchema(v)
		}
	case "array":
		s["maxItems"] = 50
		strictDelegationExecutionSchema(s["items"])
	case "string":
		s["maxLength"] = 2000
	}
}

func parseDelegationResult(raw string) (*DelegationResult, error) {
	doc := delegationObject(map[string]any{
		"status":  delegationString("completed", "partial", "needs_input", "confirmation_required", "failed"),
		"summary": delegationString(), "evidence_refs": delegationArray(delegationString(), 50),
		"actions":        delegationArray(delegationObject(map[string]any{"tool_name": delegationString(delegationBusinessNames()...), "tool_call_ref": delegationString(), "status": delegationString("success", "failed", "unknown", "not_executed")}, "tool_name", "tool_call_ref", "status"), 50),
		"missing_inputs": delegationArray(delegationObject(map[string]any{"field": delegationString(), "reason": delegationString(), "resolution": delegationString("lookup", "ask_user", "return_to_parent")}, "field", "reason", "resolution"), 20),
		"remaining_work": delegationArray(delegationString(), 20),
	}, "status", "summary", "evidence_refs", "actions", "missing_inputs", "remaining_work")
	value, err := decodeDelegationJSON(raw)
	if err != nil {
		return nil, err
	}
	if err = validateDelegationValue(value, doc, "$"); err != nil {
		return nil, err
	}
	var result DelegationResult
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	for _, ref := range result.EvidenceRefs {
		if !validDelegationRef(ref, false) {
			return nil, fmt.Errorf("evidence_refs: invalid reference")
		}
	}
	for _, action := range result.Actions {
		if !validDelegationRef(action.ToolCallRef, true) {
			return nil, fmt.Errorf("actions.tool_call_ref: invalid reference")
		}
	}
	return &result, nil
}

func asDelegationSchema(value any) delegationSchema {
	switch v := value.(type) {
	case delegationSchema:
		return v
	case map[string]any:
		return delegationSchema(v)
	default:
		return nil
	}
}

// Token decoding rejects duplicate keys, including nested keys, and retains
// integer IDs as json.Number instead of rounding them through float64.
func decodeDelegationJSON(raw string) (any, error) {
	if len(raw) > 32*1024 {
		return nil, fmt.Errorf("$: payload exceeds 32 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	value, err := readDelegationJSON(decoder, "$", 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("$: expected one JSON value")
	}
	return value, nil
}

func readDelegationJSON(decoder *json.Decoder, path string, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("%s: nesting exceeds 32", path)
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		result := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("%s: invalid key", path)
			}
			if _, exists := result[key]; exists {
				return nil, fmt.Errorf("%s.%s: duplicate field", path, key)
			}
			value, err := readDelegationJSON(decoder, path+"."+key, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return result, nil
	case '[':
		result := []any{}
		for decoder.More() {
			value, err := readDelegationJSON(decoder, fmt.Sprintf("%s[%d]", path, len(result)), depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s: invalid delimiter", path)
	}
}

func validateDelegationValue(value any, document any, path string) error {
	s := asDelegationSchema(document)
	if branches, ok := s["anyOf"].([]any); ok {
		var failures []string
		for _, branch := range branches {
			if err := validateDelegationValue(value, branch, path); err == nil {
				return nil
			} else {
				failures = append(failures, err.Error())
			}
		}
		return fmt.Errorf("%s: no permitted type matched (%s)", path, strings.Join(failures, "; "))
	}
	enum := delegationSlice(s["enum"])
	if len(enum) > 0 {
		found := false
		for _, allowed := range enum {
			if delegationEnumEqual(value, allowed) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: invalid enum value", path)
		}
	}
	switch s["type"] {
	case "null":
		if value != nil {
			return fmt.Errorf("%s: expected null", path)
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		properties := asDelegationSchema(s["properties"])
		for _, required := range delegationSlice(s["required"]) {
			key := fmt.Sprint(required)
			if _, ok := object[key]; !ok {
				return fmt.Errorf("%s.%s: required", path, key)
			}
		}
		for key, child := range object {
			property, exists := properties[key]
			if !exists {
				return fmt.Errorf("%s.%s: unknown field", path, key)
			}
			if err := validateDelegationValue(child, property, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", path)
		}
		if limit := delegationLimit(s["maxItems"]); limit > 0 && len(array) > limit {
			return fmt.Errorf("%s: exceeds %d items", path, limit)
		}
		for i, child := range array {
			if err := validateDelegationValue(child, s["items"], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: expected string", path)
		}
		if delegationLimit(s["minLength"]) > 0 && strings.TrimSpace(str) == "" {
			return fmt.Errorf("%s: must not be blank", path)
		}
		if limit := delegationLimit(s["maxLength"]); limit > 0 && utf8.RuneCountInString(str) > limit {
			return fmt.Errorf("%s: exceeds %d characters", path, limit)
		}
	case "integer", "number":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s: expected number", path)
		}
		finite, err := strconv.ParseFloat(string(number), 64)
		if err != nil || math.IsInf(finite, 0) || math.IsNaN(finite) {
			return fmt.Errorf("%s: number outside supported range", path)
		}
		// ParseFloat permits extreme negative exponents as zero; bound the
		// exponent before big.Rat to avoid allocating an attacker-sized denominator.
		if i := strings.IndexAny(string(number), "eE"); i >= 0 {
			exponent, err := strconv.Atoi(string(number)[i+1:])
			if err != nil || exponent < -400 || exponent > 400 {
				return fmt.Errorf("%s: unsupported exponent", path)
			}
		}
		rational, ok := new(big.Rat).SetString(string(number))
		if !ok {
			return fmt.Errorf("%s: invalid number", path)
		}
		if s["type"] == "integer" && !rational.IsInt() {
			return fmt.Errorf("%s: expected integer", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", path)
		}
	default:
		return fmt.Errorf("%s: unsupported schema type", path)
	}
	return nil
}

func delegationEnumEqual(value, allowed any) bool {
	if number, ok := value.(json.Number); ok {
		if err := validateDelegationValue(number, delegationSchema{"type": "number"}, "enum"); err != nil {
			return false
		}
		rightRaw, err := json.Marshal(allowed)
		if err != nil {
			return false
		}
		left, leftOK := new(big.Rat).SetString(string(number))
		right, rightOK := new(big.Rat).SetString(string(rightRaw))
		return leftOK && rightOK && left.Cmp(right) == 0
	}
	return reflect.DeepEqual(value, allowed)
}

func delegationSlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []string:
		result := make([]any, len(v))
		for i, s := range v {
			result[i] = s
		}
		return result
	default:
		return nil
	}
}
func delegationLimit(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
