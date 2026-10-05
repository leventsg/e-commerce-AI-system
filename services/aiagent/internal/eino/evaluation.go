package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	rows "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/evaluation"
	memory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"
	tools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

type EvaluationFailure struct {
	Tool string `json:"tool"`
	Call int    `json:"call"`
	Kind string `json:"kind"`
}

// EvaluationSchema exposes the production tool and Agent contract for dataset
// validation without requiring model credentials or opening network clients.
func EvaluationSchema() map[string]any {
	catalog := tools.DefaultBusinessTools(tools.DefaultToolClients{}, config.ToolTimeoutConfig{})
	definitions := map[string]any{}
	schemas := map[string]any{}
	for _, t := range catalog {
		definitions[t.Name] = map[string]any{"parameters": t.Params, "metadata": t.Metadata}
		schemas[t.Name] = evaluationJSONSchema(t.Params)
	}
	agents := map[string]any{}
	for _, a := range supervisorSubAgentSpecs {
		agents[a.name] = a.tools
	}
	return map[string]any{"schemas": schemas, "tools": definitions, "agents": agents, "protocol_version": 1}
}

func evaluationJSONSchema(params map[string]*schema.ParameterInfo) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for k, p := range params {
		properties[k] = evaluationValueSchema(p)
		if p.Required {
			required = append(required, k)
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func evaluationValueSchema(p *schema.ParameterInfo) map[string]any {
	v := map[string]any{"type": string(p.Type)}
	if p.Type == schema.Object {
		return evaluationJSONSchema(p.SubParams)
	}
	if p.ElemInfo != nil {
		v["items"] = evaluationValueSchema(p.ElemInfo)
	}
	return v
}

type EvaluationRequest struct {
	ExperimentID   string              `json:"experiment_id,omitempty"`
	RootTraceID    string              `json:"root_trace_id,omitempty"`
	Op             string              `json:"op"`
	TrialID        string              `json:"trial_id"`
	Fixture        json.RawMessage     `json:"fixture"`
	Seed           int64               `json:"seed"`
	Failures       []EvaluationFailure `json:"failures"`
	Content        string              `json:"content"`
	ConfirmationID string              `json:"confirmation_id"`
	Approved       bool                `json:"approved"`
	// This is a harness identity, never a model argument. A distinct value on
	// confirm enables explicit cross-user security tests.
	UserID         uint64 `json:"user_id,omitempty"`
	AdvanceSeconds int64  `json:"advance_seconds,omitempty"`
}
type EvaluationTrajectory struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Agent          string `json:"agent,omitempty"`
	Tool           string `json:"tool,omitempty"`
	Arguments      any    `json:"arguments,omitempty"`
	Status         string `json:"status"`
	Result         any    `json:"result,omitempty"`
	Error          string `json:"error,omitempty"`
	StartedAt      string `json:"started_at"`
	DurationMS     int64  `json:"duration_ms"`
	ConfirmationID string `json:"confirmation_id,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	CheckpointID   string `json:"checkpoint_id,omitempty"`
	UserID         uint64 `json:"user_id,omitempty"`
}
type EvaluationResponse struct {
	TrialID        string                 `json:"trial_id"`
	ConversationID string                 `json:"conversation_id"`
	AssistantText  string                 `json:"assistant_text"`
	Events         []map[string]any       `json:"events"`
	Trajectory     []EvaluationTrajectory `json:"trajectory"`
	State          *evaluation.State      `json:"state"`
	TraceID        string                 `json:"trace_id"`
	SkillLoads     []SkillLoad            `json:"skill_loads,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

// EvaluationTrial is built exclusively by the separate evaluation executable.
// No production ServiceContext, RPC client, Redis or MySQL store is constructed.
type EvaluationTrial struct {
	mu                        sync.Mutex
	ID, ConversationID        string
	experimentID, rootTraceID string
	UserID                    uint64
	state                     *evaluation.State
	runner                    Runner
	confirmations             *confirmation.Manager
	store                     *evaluationConfirmations
	observation               *Observation
	history                   []domain.ContextMessage
	fixtureContext            []domain.ContextMessage
	trajectory                []EvaluationTrajectory
	failures                  []EvaluationFailure
	calls                     map[string]int
	clock                     time.Time
	turn                      int
	skillLoads                []SkillLoad
}

// EvaluationTrialOption 允许评测 trial 覆盖 Skill 集合（候选加载）。
type EvaluationTrialOption func(*evaluationTrialOptions)

type evaluationTrialOptions struct {
	skills SkillSet
}

// WithSkillSet 为 trial 覆盖 Skill 集合；用于修改/新增候选的 A/B 评测。
func WithSkillSet(set SkillSet) EvaluationTrialOption {
	return func(opts *evaluationTrialOptions) { opts.skills = set }
}

func NewEvaluationTrial(ctx context.Context, req EvaluationRequest, cfg config.EinoConfig, factory ModelFactory, observation *Observation, options ...EvaluationTrialOption) (*EvaluationTrial, error) {
	if strings.TrimSpace(req.TrialID) == "" {
		return nil, fmt.Errorf("trial_id required")
	}
	state, err := evaluation.NewState(req.Fixture)
	if err != nil {
		return nil, err
	}
	for _, f := range req.Failures {
		if f.Call < 1 {
			return nil, fmt.Errorf("failure call must be positive")
		}
		switch f.Kind {
		case "timeout", "business_failure", "outcome_unknown", "audit_failure":
		default:
			return nil, fmt.Errorf("unknown failure kind %q", f.Kind)
		}
	}
	var trialOptions evaluationTrialOptions
	for _, option := range options {
		if option != nil {
			option(&trialOptions)
		}
	}
	t := &EvaluationTrial{ID: req.TrialID, experimentID: req.ExperimentID, rootTraceID: req.RootTraceID, ConversationID: "eval_" + req.TrialID, UserID: req.UserID, state: state, observation: observation, failures: req.Failures, calls: map[string]int{}, clock: time.Now(), trajectory: []EvaluationTrajectory{}}
	if t.UserID == 0 {
		t.UserID = 7
	}
	var fixture struct {
		Context []domain.ContextMessage `json:"context"`
		History []domain.ContextMessage `json:"history"`
	}
	_ = json.Unmarshal(req.Fixture, &fixture)
	t.fixtureContext = fixture.Context
	t.history = fixture.History
	catalog := tools.DefaultBusinessTools(tools.DefaultToolClients{}, config.ToolTimeoutConfig{})
	for i := range catalog {
		definition := catalog[i]
		catalog[i].Handler = t.handler(definition)
		if definition.Metadata.RequireConfirmation {
			catalog[i].ConfirmationSummary = func(ctx context.Context, r core.ExecuteRequest) (string, error) {
				if e := validateEvaluationParams(r.Arguments, definition.Params); e != nil {
					return "", e
				}
				return "Confirm " + r.ToolName, nil
			}
		}
	}
	capabilities := tools.DefaultCapabilityTools(tools.CapabilityDeps{MemoryProvider: t, ToolCallStore: &contextmanager.ToolCallStore{}})
	for i := range capabilities {
		if capabilities[i].Name == domain.ToolGetToolCallResult {
			capabilities[i].Handler = t.lookupToolResult
		}
	}
	catalog = append(catalog, capabilities...)
	registry := tools.NewRegistry(catalog)
	for _, f := range req.Failures {
		if _, e := registry.Metadata(f.Tool); e != nil {
			return nil, fmt.Errorf("failure tool: %w", e)
		}
	}
	executor := tools.NewExecutor(registry, tools.WithToolCallRecorder(t))
	t.store = &evaluationConfirmations{data: map[string]*rows.AiConfirmations{}}
	sequence := 0
	t.confirmations = confirmation.NewManager(t.store, registry, nil, confirmation.WithClock(func() time.Time { return t.clock }), confirmation.WithIDGenerator(func() string { sequence++; return fmt.Sprintf("%s_confirm_%d", t.ID, sequence) }))
	if factory == nil {
		factory = NewModelFactory()
	}
	skillOverrideArg := &skillOverride{set: trialOptions.skills, onGet: func(name, hash string) {
		t.mu.Lock()
		t.skillLoads = append(t.skillLoads, SkillLoad{Name: name, Hash: hash})
		t.mu.Unlock()
	}}
	t.runner, err = NewSupervisorAgent(ctx, factory, cfg, registry, WithApprovalManager(tools.NewApprovalManager(registry, executor, t.confirmations)), WithCheckpointStore(newMemoryCheckpointStore()), WithMemoryProvider(t), withSkillOverride(skillOverrideArg))
	if err != nil {
		return nil, err
	}
	return t, nil
}
func (t *EvaluationTrial) append(e EvaluationTrajectory) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.ID = fmt.Sprintf("%s_e%d", t.ID, len(t.trajectory)+1)
	if e.StartedAt == "" {
		e.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	t.trajectory = append(t.trajectory, e)
}
func (t *EvaluationTrial) handler(def core.Tool) core.HandlerFunc {
	return func(ctx context.Context, r core.HandlerRequest) (core.HandlerResult, error) {
		started := time.Now()
		execution, _ := helper.ToolExecutionFromContext(ctx)
		if err := validateEvaluationParams(r.Arguments, def.Params); err != nil {
			t.append(EvaluationTrajectory{Kind: "guard", Tool: r.ToolName, Arguments: r.Arguments, Status: "blocked", Error: err.Error(), UserID: r.UserID, ToolCallID: execution.ToolCallID})
			return core.HandlerResult{}, fmt.Errorf("%w: %v", helper.ErrInvalidToolArguments, err)
		}
		t.append(EvaluationTrajectory{Kind: "guard", Tool: r.ToolName, Status: "passed", UserID: r.UserID, ToolCallID: execution.ToolCallID})
		confirmationID, _ := ctx.Value(evaluationConfirmationKey{}).(string)
		t.mu.Lock()
		t.calls[r.ToolName]++
		call := t.calls[r.ToolName]
		t.mu.Unlock()
		kind := ""
		for _, f := range t.failures {
			if f.Tool == r.ToolName && f.Call == call {
				kind = f.Kind
			}
		}
		t.append(EvaluationTrajectory{Kind: "handler", ConfirmationID: confirmationID, Tool: r.ToolName, Arguments: r.Arguments, Status: "started", ToolCallID: execution.ToolCallID, UserID: r.UserID})
		if kind == "timeout" {
			<-ctx.Done()
			t.append(EvaluationTrajectory{Kind: "handler", ConfirmationID: confirmationID, Tool: r.ToolName, Status: "failed", Error: "injected timeout", ToolCallID: execution.ToolCallID})
			return core.HandlerResult{}, ctx.Err()
		}
		if kind == "business_failure" {
			t.append(EvaluationTrajectory{Kind: "handler", ConfirmationID: confirmationID, Tool: r.ToolName, Status: "failed", Error: "injected business failure", ToolCallID: execution.ToolCallID})
			return core.HandlerResult{}, &helper.RPCStatusError{Operation: r.ToolName, StatusCode: 500, StatusMsg: "injected business failure"}
		}
		t.mu.Lock()
		result, err := t.state.Apply(r.ToolName, r.Arguments, r.UserID)
		t.mu.Unlock()
		status := "success"
		if kind == "outcome_unknown" {
			status = "unknown"
		}
		if err != nil {
			status = "failed"
		}
		t.append(EvaluationTrajectory{Kind: "handler", ConfirmationID: confirmationID, Tool: r.ToolName, Arguments: r.Arguments, Status: status, Result: result, DurationMS: time.Since(started).Milliseconds(), ToolCallID: execution.ToolCallID, UserID: r.UserID})
		if kind == "outcome_unknown" && err == nil {
			return core.HandlerResult{}, helper.ErrWriteRPCOutcomeUnknown
		}
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("%w: %v", helper.ErrRPCBusinessStatus, err)
		}
		return core.HandlerResult{Data: result, Summary: "Simulated business operation completed"}, nil
	}
}
func (t *EvaluationTrial) RecordToolCall(ctx context.Context, r tools.ToolCallRecord) error {
	confirmationID, _ := ctx.Value(evaluationConfirmationKey{}).(string)
	var result any
	b, _ := json.Marshal(r.ResultData)
	_ = json.Unmarshal(b, &result)
	if s, ok := result.(string); ok {
		_ = json.Unmarshal([]byte(s), &result)
	}
	status := r.Status
	if m, ok := result.(map[string]any); ok && m["type"] == domain.EventConfirmationRequired {
		status = "pending"
	}
	t.append(EvaluationTrajectory{Kind: "tool", ConfirmationID: confirmationID, Tool: r.ToolName, Arguments: r.Arguments, Status: status, Result: result, Error: r.ErrorMessage, DurationMS: r.Latency.Milliseconds(), ToolCallID: r.ToolCallID, UserID: r.UserID})
	if r.Metadata.WriteOperation {
		for _, f := range t.failures {
			if f.Tool == r.ToolName && f.Call == t.calls[r.ToolName] && f.Kind == "audit_failure" {
				t.append(EvaluationTrajectory{Kind: "guard", Tool: r.ToolName, Status: "audit_failed", Error: "injected audit failure", ToolCallID: r.ToolCallID})
				return fmt.Errorf("injected audit failure")
			}
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		for _, f := range t.failures {
			if f.Tool == r.ToolName && f.Call == t.calls[r.ToolName] && f.Kind == "audit_failure" {
				return fmt.Errorf("injected audit failure")
			}
		}
		t.state.Audit = append(t.state.Audit, evaluation.Object{"tool": r.ToolName, "status": r.Status, "user_id": r.UserID, "arguments": r.Arguments, "tool_call_id": r.ToolCallID})
	}
	return nil
}
func (t *EvaluationTrial) Retrieve(_ context.Context, r *memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	request := ""
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			request = r.Messages[i].Content
			break
		}
	}
	t.append(EvaluationTrajectory{Kind: "agent", Agent: r.AgentName, Arguments: map[string]any{"request": request}, Status: "started", RunID: r.RunID, UserID: r.UserID})
	return &memory.RetrieveResult{ContextMessages: t.fixtureContext}, nil
}
func (t *EvaluationTrial) Memorize(context.Context, *memory.MemorizeRequest) error { return nil }
func (t *EvaluationTrial) Close() error                                            { return nil }
func (t *EvaluationTrial) SearchUserMemoryEvents(_ context.Context, q memory.UserMemoryEventQuery) ([]memory.Event, error) {
	if q.UserID != t.UserID {
		return nil, fmt.Errorf("memory access forbidden")
	}
	return []memory.Event{}, nil
}
func (t *EvaluationTrial) lookupToolResult(_ context.Context, r core.HandlerRequest) (core.HandlerResult, error) {
	if r.UserID != t.UserID || r.ConversationID != t.ConversationID {
		return core.HandlerResult{}, fmt.Errorf("tool result access forbidden")
	}
	id, _ := r.Arguments["tool_call_id"].(string)
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.trajectory) - 1; i >= 0; i-- {
		e := t.trajectory[i]
		if id != "" && e.Kind == "tool" && e.ToolCallID == id {
			return core.HandlerResult{Data: map[string]any{"tool_call_id": id, "tool_name": e.Tool, "arguments": e.Arguments, "status": e.Status, "result": e.Result}, Summary: "Recorded simulated tool result"}, nil
		}
	}
	return core.HandlerResult{}, fmt.Errorf("tool result not found")
}

func (t *EvaluationTrial) Execute(ctx context.Context, req EvaluationRequest) EvaluationResponse {
	response := EvaluationResponse{TrialID: t.ID, ConversationID: t.ConversationID, Events: []map[string]any{}}
	if req.TrialID != t.ID {
		response.Error = "trial_id mismatch"
		return response
	}
	if req.AdvanceSeconds < 0 {
		response.Error = "clock cannot move backwards"
		return response
	}
	t.clock = t.clock.Add(time.Duration(req.AdvanceSeconds) * time.Second)
	ctx, end := t.observation.Start(ctx, "evaluation."+req.Op, t.UserID, t.ConversationID, req.Content)
	defer func() { end(response.AssistantText) }()
	ctx = obs.Correlate(ctx, map[string]string{"trial_id": t.ID, "experiment_id": t.experimentID, "evaluation_root_trace_id": t.rootTraceID})
	response.TraceID = obs.TraceID(ctx)
	var events []domain.AgentEvent
	var err error
	switch req.Op {
	case "init", "end":
	case "chat":
		t.turn++
		t.history = append(t.history, domain.ContextMessage{Role: "user", Content: req.Content})
		events, err = t.runner.Run(ctx, RunRequest{UserID: t.UserID, ConversationID: t.ConversationID, MessageID: fmt.Sprintf("%s_turn_%d", t.ID, t.turn), Messages: t.history})
	case "confirm":
		user := t.UserID
		if req.UserID != 0 {
			user = req.UserID
		}
		var decided *domain.Confirmation
		decided, err = t.confirmations.Decide(ctx, confirmation.DecisionRequest{UserID: user, ConversationID: t.ConversationID, ConfirmationID: req.ConfirmationID, Approved: req.Approved})
		status := "rejected"
		if req.Approved {
			status = "approved"
		}
		if err != nil {
			status = "blocked"
		}
		toolName := ""
		if decided != nil {
			toolName = decided.ToolName
		}
		t.append(EvaluationTrajectory{Kind: "confirmation", Tool: toolName, Status: status, ConfirmationID: req.ConfirmationID, UserID: user})
		ctx = context.WithValue(ctx, evaluationConfirmationKey{}, req.ConfirmationID)
		if err == nil && req.Approved {
			events, err = t.runner.Resume(ctx, ResumeRequest{UserID: user, ConversationID: t.ConversationID, ConfirmationID: decided.ID, RunID: decided.RunID, CheckpointID: decided.CheckpointID, InterruptID: decided.InterruptID, Approved: true})
			complete := confirmation.CompletionRequest{UserID: user, ConversationID: t.ConversationID, ConfirmationID: decided.ID}
			executed := false
			for _, e := range events {
				if e.BusinessExecuted || (e.Type == domain.EventToolResult && e.Tool == decided.ToolName && e.Status == "success") {
					executed = true
				}
			}
			if executed {
				_, markErr := t.confirmations.MarkExecuted(ctx, complete)
				if err == nil {
					err = markErr
				}
			} else {
				_, markErr := t.confirmations.MarkFailed(ctx, complete)
				if err == nil {
					err = markErr
				}
			}
		} else if err == nil {
			events = []domain.AgentEvent{{Type: domain.EventAssistantMessage, Content: "Operation cancelled; no business action was executed.", Done: true}}
		}
	default:
		err = fmt.Errorf("unknown operation %q", req.Op)
	}
	for _, e := range events {
		// Reasoning deltas are never part of evaluation evidence or judge input.
		if e.Type == domain.EventAssistantThinkingDelta || e.Type == "a2ui" {
			continue
		}
		if e.Type == domain.EventToolProgress {
			var args any
			_ = json.Unmarshal([]byte(e.DataJSON), &args)
			t.append(EvaluationTrajectory{Kind: "tool_request", Tool: e.Tool, ToolCallID: e.ToolCallID, Arguments: args, Status: "requested"})
		}
		data := map[string]any{"type": e.Type, "content": e.Content, "tool": e.Tool, "status": e.Status, "confirmation_id": e.ConfirmationID, "tool_call_id": e.ToolCallID, "done": e.Done, "business_executed": e.BusinessExecuted}
		if e.DataJSON != "" {
			var payload any
			if json.Unmarshal([]byte(e.DataJSON), &payload) == nil {
				data["data"] = payload
			}
		}
		response.Events = append(response.Events, data)
		if e.Type == domain.EventAssistantMessage {
			response.AssistantText = e.Content
		}
		if e.Type == domain.EventConfirmationRequired {
			t.append(EvaluationTrajectory{Kind: "confirmation", Tool: e.Tool, Status: "pending", ConfirmationID: e.ConfirmationID})
		}
	}
	if response.AssistantText != "" {
		t.history = append(t.history, domain.ContextMessage{Role: "assistant", Content: response.AssistantText})
	}
	if err != nil {
		obs.Result(ctx, "failed", "")
		response.Error = err.Error()
	}
	t.mu.Lock()
	response.Trajectory = append([]EvaluationTrajectory{}, t.trajectory...)
	response.SkillLoads = append([]SkillLoad{}, t.skillLoads...)
	b, _ := json.Marshal(t.state)
	_ = json.Unmarshal(b, &response.State)
	t.mu.Unlock()
	return response
}

type evaluationConfirmationKey struct{}

func validateEvaluationParams(args map[string]any, params map[string]*schema.ParameterInfo) error {
	for name, p := range params {
		v, exists := args[name]
		if !exists {
			if p.Required {
				return fmt.Errorf("%s is required", name)
			}
			continue
		}
		if err := validateEvaluationValue(v, p); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for name := range args {
		if _, ok := params[name]; !ok {
			return fmt.Errorf("unknown parameter %s", name)
		}
	}
	return nil
}
func validateEvaluationValue(v any, p *schema.ParameterInfo) error {
	switch p.Type {
	case schema.String:
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("non-empty string required")
		}
	case schema.Integer:
		n, ok := v.(float64)
		if !ok || math.Trunc(n) != n {
			return fmt.Errorf("integer required")
		}
	case schema.Number:
		if _, ok := v.(float64); !ok {
			return fmt.Errorf("number required")
		}
	case schema.Boolean:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("boolean required")
		}
	case schema.Array:
		a, ok := v.([]any)
		if !ok {
			return fmt.Errorf("array required")
		}
		if p.ElemInfo != nil {
			for _, item := range a {
				if err := validateEvaluationValue(item, p.ElemInfo); err != nil {
					return err
				}
			}
		}
	case schema.Object:
		a, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("object required")
		}
		return validateEvaluationParams(a, p.SubParams)
	}
	return nil
}
