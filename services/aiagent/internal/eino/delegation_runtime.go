package eino

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

const delegationEvidencePrefix = "delegation.evidence/"

type delegationRunLockKey struct{}
type delegationExecutionKey struct{}

// All mutable execution state belongs to a single delegation, never the agent.
// JSON strings in the ADK session survive its native checkpoint serialization.
type delegationSnapshot struct {
	Request        *DelegationRequest            `json:"request"`
	UserID         uint64                        `json:"user_id"`
	ConversationID string                        `json:"conversation_id"`
	Evidence       []*ResolvedDelegationEvidence `json:"evidence"`
	Actions        []DelegationAction            `json:"actions"`
}

type delegationExecution struct {
	mu    sync.Mutex
	key   string
	state delegationSnapshot
}

func withDelegationRunLock(ctx context.Context) context.Context {
	return context.WithValue(ctx, delegationRunLockKey{}, &sync.Mutex{})
}

func WithDelegationEvidenceResolver(resolver *DelegationEvidenceResolver) SupervisorOption {
	return func(opts *supervisorOptions) { opts.evidenceResolver = resolver }
}

type delegationTool struct {
	inner    einotool.InvokableTool
	spec     agentSpec
	registry *aitools.Registry
	resolver *DelegationEvidenceResolver
}

func newDelegationTool(ctx context.Context, child adk.Agent, spec agentSpec, registry *aitools.Registry, resolver *DelegationEvidenceResolver) (einotool.BaseTool, error) {
	params, err := buildDelegationSchema(ctx, spec, registry)
	if err != nil {
		return nil, err
	}
	inner, ok := adk.NewAgentTool(ctx, child, adk.WithAgentInputSchema(params)).(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("agent tool %s is not invokable", spec.name)
	}
	return &delegationTool{inner: inner, spec: spec, registry: registry, resolver: resolver}, nil
}

func (t *delegationTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *delegationTool) InvokableRun(ctx context.Context, raw string, opts ...einotool.Option) (string, error) {
	ctx, end := obs.Start(ctx, "agent.delegation", map[string]string{"agent": t.spec.name})
	status := "failed"
	defer func() { end(status) }()
	req, err := parseDelegationRequest(ctx, raw, t.spec, t.registry)
	if err != nil {
		obs.Event(ctx, "delegation.validation", "blocked", map[string]string{"agent": t.spec.name, "error_code": "invalid_arguments"})
		field := strings.TrimPrefix(strings.SplitN(err.Error(), ":", 2)[0], "$.")
		return delegationMissing(field, err.Error(), "return_to_parent"), nil
	}
	identity, ok := helper.ToolExecutionFromContext(ctx)
	if !ok || identity.UserID == 0 || identity.ConversationID == "" {
		return "", helper.ErrToolExecutionContext
	}
	key := "delegation.call/" + identity.ToolCallID
	state := delegationSnapshot{Request: req, UserID: identity.UserID, ConversationID: identity.ConversationID, Evidence: []*ResolvedDelegationEvidence{}, Actions: []DelegationAction{}}
	if saved, exists := adk.GetSessionValue(ctx, key); exists {
		encoded, valid := saved.(string)
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if !valid || decoder.Decode(&state) != nil || state.Request == nil || state.UserID != identity.UserID || state.ConversationID != identity.ConversationID {
			return "", fmt.Errorf("invalid delegation checkpoint")
		}
		before, _ := json.Marshal(state.Request)
		after, _ := json.Marshal(req)
		if string(before) != string(after) {
			return delegationMissing("task", "委派调用标识已用于不同任务。", "return_to_parent"), nil
		}
		req = state.Request
	} else {
		refs := delegationReferences(req)
		if len(refs) > 20 {
			return delegationMissing("context", "证据引用总数不能超过 20。", "return_to_parent"), nil
		}
		budget := 0
		for _, ref := range refs {
			evidence, resolveErr := t.resolve(ctx, ref)
			if resolveErr != nil {
				if errors.Is(resolveErr, ErrDelegationEvidenceUnavailable) {
					return delegationMissing("context", "引用不可用："+ref, "return_to_parent"), nil
				}
				return "", resolveErr
			}
			encoded, _ := json.Marshal(evidence)
			budget += len(encoded)
			if budget > 64*1024 {
				return delegationMissing("context", "证据内容超过交接预算，请缩小任务。", "return_to_parent"), nil
			}
			state.Evidence = append(state.Evidence, evidence)
		}
		for _, action := range req.Context.PriorActions {
			matched := false
			for _, evidence := range state.Evidence {
				if evidence.Ref == action.ToolCallRef && evidence.ToolName == action.Action {
					matched = true
				}
			}
			if !matched {
				return delegationMissing("context.prior_actions", "先前动作与真实工具记录不一致。", "return_to_parent"), nil
			}
		}
	}
	for _, gap := range req.Gaps {
		if gap.Blocks == "task" && gap.Resolution != "lookup" {
			return delegationMissing(gap.Field, gap.Reason, gap.Resolution), nil
		}
	}
	execution := &delegationExecution{key: key, state: state}
	execution.save(ctx)
	ctx = context.WithValue(ctx, delegationExecutionKey{}, execution)
	// Evidence is data, never a system instruction; only its safe projection is
	// supplied to the model. The original authenticated identity stays in context.
	input, _ := json.Marshal(map[string]any{"delegation": req, "resolved_evidence": projectDelegationEvidence(state.Evidence)})
	result, err := t.inner.InvokableRun(ctx, string(input), opts...)
	if err != nil {
		return "", err
	} // Preserve native composite interrupts unchanged.
	parsed, err := parseDelegationResult(result)
	if err != nil {
		parsed = emptyDelegationResult("failed", "子任务返回格式无效；请依据已记录动作核实结果，不要自动重跑写操作。")
		parsed.RemainingWork = []string{"核实已有动作及未完成事项"}
	}
	snapshot := execution.snapshot()
	for _, claim := range parsed.Actions {
		if !slices.Contains(snapshot.Actions, claim) {
			parsed = emptyDelegationResult("failed", "子任务动作声明缺少真实执行证据。请核实已有结果。")
			break
		}
	}
	for _, ref := range parsed.EvidenceRefs {
		if _, resolveErr := t.resolve(ctx, ref); resolveErr != nil {
			if !errors.Is(resolveErr, ErrDelegationEvidenceUnavailable) {
				return "", resolveErr
			}
			parsed = emptyDelegationResult("failed", "子任务返回的证据引用不可用。请核实已有结果。")
			break
		}
	}
	parsed.Actions = snapshot.Actions
	parsed.EvidenceRefs = []string{}
	addRef := func(ref string) {
		if !slices.Contains(parsed.EvidenceRefs, ref) {
			parsed.EvidenceRefs = append(parsed.EvidenceRefs, ref)
		}
	}
	for _, evidence := range snapshot.Evidence {
		addRef(evidence.Ref)
	}
	hasSuccess, hasFailure, intentCompleted := false, false, false
	for _, action := range parsed.Actions {
		addRef(action.ToolCallRef)
		if action.Status == "success" {
			hasSuccess = true
			if action.ToolName == req.Task.Intent {
				intentCompleted = true
			}
		} else {
			hasFailure = true
		}
	}
	if hasFailure {
		parsed.Status = "failed"
		if hasSuccess {
			parsed.Status = "partial"
		}
		parsed.Summary = "部分或全部工具未成功；请依据 actions 和真实工具结果说明已完成、失败及结果未知的部分。"
	}
	if _, atomicErr := t.registry.Metadata(req.Task.Intent); atomicErr == nil && parsed.Status == "completed" && !intentCompleted {
		parsed.Status = "needs_input"
		parsed.Summary = "目标动作尚无本次成功执行证据，请核实必要信息或先前执行结果。"
	}
	// Only a real ADK interrupt can request confirmation; model text cannot.
	if parsed.Status == "confirmation_required" {
		parsed.Status = "failed"
		parsed.Summary = "没有有效的服务端确认中断，不能声明已创建确认请求。"
	}
	status = parsed.Status
	encoded, _ := json.Marshal(parsed)
	return string(encoded), nil
}

func (t *delegationTool) resolve(ctx context.Context, ref string) (*ResolvedDelegationEvidence, error) {
	identity, ok := helper.ToolExecutionFromContext(ctx)
	if !ok || identity.UserID == 0 {
		return nil, ErrDelegationEvidenceUnavailable
	}
	if strings.HasPrefix(ref, "tool_call:") {
		if raw, exists := adk.GetSessionValue(ctx, delegationEvidencePrefix+strings.TrimPrefix(ref, "tool_call:")); exists {
			var record ResolvedDelegationEvidence
			encoded, valid := raw.(string)
			if valid && json.Unmarshal([]byte(encoded), &record) == nil && record.Ref == ref && record.UserID == identity.UserID && record.ConversationID == identity.ConversationID {
				return &record, nil
			}
			return nil, ErrDelegationEvidenceUnavailable
		}
	}
	if t.resolver == nil {
		return nil, ErrDelegationEvidenceUnavailable
	}
	return t.resolver.Resolve(ctx, ref)
}

func delegationReferences(req *DelegationRequest) []string {
	refs := []string{}
	add := func(ref string) {
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	for _, item := range req.Context.Evidence {
		add(item.Ref)
	}
	for _, item := range req.Context.PriorActions {
		add(item.ToolCallRef)
	}
	for _, item := range req.Constraints {
		for _, ref := range item.SourceRefs {
			add(ref)
		}
	}
	return refs
}

func projectDelegationEvidence(records []*ResolvedDelegationEvidence) []map[string]any {
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		result = append(result, map[string]any{"ref": record.Ref, "tool_name": record.ToolName, "arguments": record.Arguments, "status": record.Status, "result": record.Result, "error": record.ErrorMessage, "role": record.Role, "content": record.Content})
	}
	return result
}

func (e *delegationExecution) snapshot() delegationSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.state
	state.Actions = append([]DelegationAction{}, e.state.Actions...)
	state.Evidence = append([]*ResolvedDelegationEvidence{}, e.state.Evidence...)
	return state
}

func (e *delegationExecution) save(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	encoded, _ := json.Marshal(e.state)
	adk.AddSessionValue(ctx, e.key, string(encoded))
}

func emptyDelegationResult(status, summary string) *DelegationResult {
	return &DelegationResult{Status: status, Summary: summary, EvidenceRefs: []string{}, Actions: []DelegationAction{}, MissingInputs: []DelegationMissingInput{}, RemainingWork: []string{}}
}

func delegationMissing(field, reason, resolution string) string {
	result := emptyDelegationResult("needs_input", reason)
	result.MissingInputs = []DelegationMissingInput{{Field: field, Reason: reason, Resolution: resolution}}
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

type delegationGuardMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	registry   *aitools.Registry
	allowSkill bool
}

func newDelegationGuardMiddleware(registry *aitools.Registry) *delegationGuardMiddleware {
	return &delegationGuardMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, registry: registry}
}

func (m *delegationGuardMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, raw string, opts ...einotool.Option) (string, error) {
		execution, _ := ctx.Value(delegationExecutionKey{}).(*delegationExecution)
		if execution == nil {
			return "", fmt.Errorf("trusted delegation context missing")
		}
		if tc == nil {
			return "", fmt.Errorf("delegation tool context missing")
		}
		ctx = withToolCallID(ctx, tc.CallID)
		// Only the configured official read-only Skill loader is outside Registry.
		if tc.Name == "skill" && m.allowSkill {
			return endpoint(ctx, raw, opts...)
		}
		lock, _ := ctx.Value(delegationRunLockKey{}).(*sync.Mutex)
		if lock == nil {
			return "", fmt.Errorf("delegation run lock missing")
		}
		callKey := "delegation.tool_call/" + tc.CallID
		ownsCall := false
		lock.Lock()
		_, hasEvidence := adk.GetSessionValue(ctx, delegationEvidencePrefix+tc.CallID)
		_, hasReservation := adk.GetSessionValue(ctx, callKey)
		wasInterrupted, _, _ := einotool.GetInterruptState[string](ctx)
		lock.Unlock()
		if hasEvidence || (hasReservation && !wasInterrupted) {
			obs.Event(ctx, "delegation.guard", "blocked", map[string]string{"tool": tc.Name, "error_code": "duplicate_call_id"})
			return delegationBlocked(tc.Name, "duplicate_call_id", "工具调用标识已使用，请核实先前结果。"), nil
		}
		state := execution.snapshot()
		metadata, err := m.registry.Metadata(tc.Name)
		if err != nil {
			return "", err
		}
		args := map[string]any{}
		block := func(code, message string) (string, error) {
			lock.Lock()
			_, recorded := adk.GetSessionValue(ctx, delegationEvidencePrefix+tc.CallID)
			_, reserved := adk.GetSessionValue(ctx, callKey)
			if recorded || (reserved && !ownsCall) {
				code = "duplicate_call_id"
				message = "工具调用标识已使用，请核实先前结果。"
			}
			if code != "duplicate_call_id" && !reserved {
				adk.AddSessionValue(ctx, callKey, "rejected")
				ownsCall = true
			}
			lock.Unlock()
			if code == "duplicate_call_id" {
				obs.Event(ctx, "delegation.guard", "blocked", map[string]string{"tool": tc.Name, "error_code": code})
				return delegationBlocked(tc.Name, code, message), nil
			}
			result, rejectErr := m.registry.RejectDelegation(ctx, tc.Name, args, code, message)
			if rejectErr != nil {
				return "", rejectErr
			}
			clean, _ := json.Marshal(argx.SanitizeMapKeys(args, []string{"user_id", "token", "auth"}))
			_, recordErr := recordDelegationResult(ctx, execution, tc, clean, result)
			obs.Event(ctx, "delegation.guard", "blocked", map[string]string{"tool": tc.Name, "error_code": code})
			return result, recordErr
		}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err = decoder.Decode(&args); err != nil {
			return block("invalid_arguments", "工具参数不是合法 JSON。")
		}
		args = argx.SanitizeMapKeys(args, []string{"user_id", "token", "auth"})
		if slices.Contains(state.Request.Scope.Exclusions, tc.Name) || (metadata.WriteOperation && state.Request.Scope.Mode != "execute") {
			return block("scope_denied", "本次委派不允许执行该动作。")
		}
		for _, gap := range state.Request.Gaps {
			if gap.Resolution != "lookup" && (gap.Blocks == "task" || metadata.WriteOperation) {
				return block("missing_input", gap.Field+": 必须先补齐信息。")
			}
			if metadata.WriteOperation && gap.Resolution == "lookup" && !delegationLookupResolved(state, gap.Field, args) {
				return block("missing_input", gap.Field+": 尚无唯一匹配的查询证据。")
			}
		}
		if metadata.WriteOperation {
			if conflict := delegationInputConflict(state.Request.Inputs, args); conflict != "" {
				return block("input_conflict", conflict+": 工具参数与委派已确定的输入冲突。")
			}
			if intentMeta, intentErr := m.registry.Metadata(state.Request.Task.Intent); intentErr != nil || !intentMeta.WriteOperation || (tc.Name != state.Request.Task.Intent && !(state.Request.Task.Intent == "order_create" && tc.Name == "checkout_prepare")) {
				return block("intent_denied", "该写操作超出本次委派意图。")
			}
		}
		if err := validateDelegationToolArguments(ctx, m.registry, tc.Name, args); err != nil {
			return block("invalid_arguments", err.Error())
		}
		clean, _ := json.Marshal(args)
		clean = canonicalDelegationJSON(args)
		callOwner := execution.key + "/" + tc.Name + "/" + string(clean)
		lock.Lock()
		heldCall, reused := adk.GetSessionValue(ctx, callKey)
		interrupted, _, _ := einotool.GetInterruptState[string](ctx)
		if reused && !(heldCall == callOwner && interrupted) {
			lock.Unlock()
			return block("duplicate_call_id", "工具调用标识已使用，请核实先前结果。")
		}
		adk.AddSessionValue(ctx, callKey, callOwner)
		ownsCall = true
		lock.Unlock()
		fingerprint := fmt.Sprintf("delegation.write/%x", sha256.Sum256(append([]byte(tc.Name+":"), clean...)))
		if metadata.WriteOperation {
			lock.Lock()
			blocked := false
			if held, exists := adk.GetSessionValue(ctx, fingerprint); exists && held != "" {
				interrupted, _, _ := einotool.GetInterruptState[string](ctx)
				blocked = !(held == tc.CallID && interrupted)
			}
			for _, evidence := range state.Evidence {
				if evidence.ToolName != tc.Name {
					continue
				}
				var priorArgs map[string]any
				d := json.NewDecoder(strings.NewReader(string(evidence.Arguments)))
				d.UseNumber()
				if d.Decode(&priorArgs) != nil {
					blocked = true
					continue
				}
				priorJSON := canonicalDelegationJSON(argx.SanitizeMapKeys(priorArgs, []string{"user_id", "token", "auth"}))
				if string(priorJSON) == string(clean) && delegationEvidenceMayHaveExecuted(evidence) {
					blocked = true
				}
			}
			if !blocked {
				adk.AddSessionValue(ctx, fingerprint, tc.CallID)
			}
			lock.Unlock()
			if blocked {
				return block("duplicate_write", "相同写操作已完成、处理中或结果未知，请先核实原结果。")
			}
		}
		result, err := endpoint(ctx, string(clean), opts...)
		if err != nil {
			return "", err
		}
		record, recordErr := recordDelegationResult(ctx, execution, tc, clean, result)
		if recordErr != nil {
			return "", recordErr
		}
		if metadata.WriteOperation && !delegationEvidenceMayHaveExecuted(record) {
			adk.AddSessionValue(ctx, fingerprint, "")
		}
		return result, nil
	}, nil
}

func recordDelegationResult(ctx context.Context, execution *delegationExecution, tc *adk.ToolContext, clean []byte, result string) (*ResolvedDelegationEvidence, error) {
	// Tool IDs originate at the model boundary. Never let a reused ID replace
	// existing server evidence, including replacing success with a later denial.
	if lock, ok := ctx.Value(delegationRunLockKey{}).(*sync.Mutex); ok {
		lock.Lock()
		defer lock.Unlock()
	}
	if previous, exists := adk.GetSessionValue(ctx, delegationEvidencePrefix+tc.CallID); exists {
		var record ResolvedDelegationEvidence
		encoded, ok := previous.(string)
		if !ok || json.Unmarshal([]byte(encoded), &record) != nil {
			return nil, fmt.Errorf("invalid saved tool evidence")
		}
		return &record, nil
	}
	identity, _ := helper.ToolExecutionFromContext(ctx)
	var envelope struct {
		Status          string `json:"status"`
		BusinessOutcome string `json:"business_outcome"`
	}
	_ = json.Unmarshal([]byte(result), &envelope)
	actionStatus := "failed"
	switch {
	case envelope.BusinessOutcome == "unknown":
		actionStatus = "unknown"
	case envelope.Status == "success":
		actionStatus = "success"
	case envelope.BusinessOutcome == "not_executed" || envelope.Status == "rejected":
		actionStatus = "not_executed"
	case envelope.BusinessOutcome == "executed":
		actionStatus = "failed"
	}
	record := &ResolvedDelegationEvidence{Ref: "tool_call:" + tc.CallID, UserID: identity.UserID, ConversationID: identity.ConversationID, ToolName: tc.Name, Arguments: clean, Status: envelope.Status, Result: json.RawMessage(result)}
	encoded, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		return nil, fmt.Errorf("invalid tool evidence: %w", marshalErr)
	}
	adk.AddSessionValue(ctx, delegationEvidencePrefix+tc.CallID, string(encoded))
	execution.mu.Lock()
	if slices.Contains(delegationBusinessNames(), tc.Name) {
		execution.state.Actions = append(execution.state.Actions, DelegationAction{ToolName: tc.Name, ToolCallRef: record.Ref, Status: actionStatus})
	}
	execution.state.Evidence = append(execution.state.Evidence, record)
	execution.mu.Unlock()
	execution.save(ctx)
	return record, nil
}

func delegationInputConflict(inputs, args map[string]any) string {
	for key, value := range inputs {
		actual, exists := args[key]
		if !exists || value == nil {
			continue
		}
		left := canonicalDelegationJSON(value)
		right := canonicalDelegationJSON(actual)
		if string(left) != string(right) {
			return "inputs." + key
		}
	}
	return ""
}

func canonicalDelegationJSON(value any) []byte {
	var normalize func(any) any
	normalize = func(value any) any {
		switch v := value.(type) {
		case json.Number:
			if validateDelegationValue(v, delegationSchema{"type": "number"}, "number") != nil {
				return v
			}
			rational, _ := new(big.Rat).SetString(string(v))
			if rational.IsInt() {
				return json.Number(rational.Num().String())
			}
			number, _ := v.Float64()
			return number
		case map[string]any:
			out := make(map[string]any, len(v))
			for key, item := range v {
				out[key] = normalize(item)
			}
			return out
		case []any:
			out := make([]any, len(v))
			for i, item := range v {
				out[i] = normalize(item)
			}
			return out
		default:
			return value
		}
	}
	encoded, _ := json.Marshal(normalize(value))
	return encoded
}

// A lookup gap is closed only by one distinct value in successful tool evidence.
// Multiple candidates require a new delegation after the user has selected one.
func delegationLookupResolved(state delegationSnapshot, path string, args map[string]any) bool {
	path = strings.TrimPrefix(path, "inputs.")
	if strings.Contains(path, ".") || strings.Contains(path, "[") {
		return false
	}
	actual, exists := args[path]
	if !exists || actual == nil {
		return false
	}
	values := map[string]bool{}
	var collect func(any)
	collect = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == path && item != nil {
					encoded, _ := json.Marshal(item)
					values[string(encoded)] = true
				}
				collect(item)
			}
		case []any:
			for _, item := range v {
				collect(item)
			}
		}
	}
	for _, record := range state.Evidence {
		if record.Status != "success" {
			continue
		}
		var decoded any
		d := json.NewDecoder(strings.NewReader(string(record.Result)))
		d.UseNumber()
		if d.Decode(&decoded) == nil {
			collect(decoded)
		}
	}
	encoded, _ := json.Marshal(actual)
	return len(values) == 1 && values[string(encoded)]
}

func delegationEvidenceMayHaveExecuted(record *ResolvedDelegationEvidence) bool {
	var envelope struct {
		BusinessOutcome string `json:"business_outcome"`
		Type            string `json:"type"`
	}
	_ = json.Unmarshal(record.Result, &envelope)
	if envelope.Type == "confirmation_required" || envelope.BusinessOutcome == "not_executed" {
		return false
	}
	return record.Status == "success" || envelope.BusinessOutcome == "unknown" || envelope.BusinessOutcome == "executed"
}

func delegationBlocked(toolName, code, message string) string {
	encoded, _ := json.Marshal(map[string]any{"tool_name": toolName, "status": "failed", "business_outcome": "not_executed", "error": map[string]any{"code": code, "message": message, "retryable_in_current_run": false}})
	return string(encoded)
}
