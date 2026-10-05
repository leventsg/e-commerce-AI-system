// Package evolution implements the minimal Self-Evolving pipeline: read redacted
// production evidence, derive one Skill candidate, and run a fixed Skill-specific
// A/B evaluation against an independently maintained dataset. It is a local,
// single-run command; it adds no database, worker, queue or admin page.
package evolution

import "encoding/json"

// Dataset is the independently maintained, version-pinned Skill-specific test
// set. It is never read by the candidate generator and is not produced from the
// generator's own output.
type Dataset struct {
	Version string `json:"version"`
	Cases   []Case `json:"cases"`
}

// Case is a single skill-specific evaluation case with a fixed synthetic fixture.
type Case struct {
	ID               string          `json:"id"`
	Capability       string          `json:"capability"`
	TargetAgent      string          `json:"target_agent"`
	SkillName        string          `json:"skill_name"`
	Category         string          `json:"category"`           // applicable | not_applicable | boundary
	ExpectedSkillUse string          `json:"expected_skill_use"` // required | forbidden | optional
	Input            string          `json:"input"`
	Fixture          json.RawMessage `json:"fixture"`
	BehaviorChecks   []BehaviorCheck `json:"behavior_checks"`
	ForbiddenActions []ForbiddenAction `json:"forbidden_actions,omitempty"`
	ConfirmationPolicy string        `json:"confirmation_policy,omitempty"`
	SourceRefs       []string        `json:"source_refs,omitempty"`
}

// BehaviorCheck is either a deterministic, executable assertion or a semantic
// requirement that only the independent judge may resolve.
type BehaviorCheck struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"` // deterministic | semantic
	Description string         `json:"description"`
	Check       map[string]any `json:"check,omitempty"`
}

// ForbiddenAction declares an action that must never occur for a case.
type ForbiddenAction struct {
	Kind  string `json:"kind"` // tool | write
	Value string `json:"value,omitempty"`
}

// Candidate is the single skill candidate produced or loaded for one run.
type Candidate struct {
	Operation         string              `json:"operation"` // add | modify
	SkillName         string              `json:"skill_name"`
	TargetAgent       string              `json:"target_agent"`
	Capability        string              `json:"capability,omitempty"`
	BaselineSkillHash string              `json:"baseline_skill_hash,omitempty"`
	SkillMarkdown     string              `json:"skill_markdown"`
	Reason            string              `json:"reason,omitempty"`
	SourceTraceIDs    []string            `json:"source_trace_ids,omitempty"`
	Bindings          map[string][]string `json:"bindings,omitempty"`
	GeneratedAt       string              `json:"generated_at,omitempty"`
}

// SessionAnalysis is the independent conclusion for one production session.
type SessionAnalysis struct {
	TraceID       string   `json:"trace_id"`
	SessionID     string   `json:"session_id,omitempty"`
	Agent         string   `json:"agent,omitempty"`
	Conclusion    string   `json:"conclusion"` // success | failure | appropriate_stop | unknown
	Goal          string   `json:"goal,omitempty"`
	SkillGap      bool     `json:"skill_gap,omitempty"`
	ReusableStep  string   `json:"reusable_step,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	SkipReason    string   `json:"skip_reason,omitempty"`
}

// AnalysisOutput summarizes the production analysis and the resulting gap or
// reusable pattern that justifies a candidate.
type AnalysisOutput struct {
	WindowFrom     string            `json:"window_from,omitempty"`
	WindowTo       string            `json:"window_to,omitempty"`
	TargetAgent    string            `json:"target_agent,omitempty"`
	MaxSessions    int               `json:"max_sessions,omitempty"`
	TruncatedRange bool              `json:"truncated_range,omitempty"`
	Sessions       []SessionAnalysis `json:"sessions"`
	Skips          []SessionAnalysis `json:"skips,omitempty"`
	Gap            string            `json:"gap,omitempty"`
	SuccessPattern string            `json:"success_pattern,omitempty"`
}

// SessionEvidence is a redacted production session normalized for analysis. It is
// the learning source and must never be confused with the evaluation dataset.
type SessionEvidence struct {
	TraceID       string            `json:"trace_id"`
	SessionID     string            `json:"session_id,omitempty"`
	Agent         string            `json:"agent,omitempty"`
	Input         string            `json:"input,omitempty"`
	Output        string            `json:"output,omitempty"`
	ToolCalls     []ToolEvidence    `json:"tool_calls,omitempty"`
	Confirmations []ConfirmationEvidence `json:"confirmations,omitempty"`
	Truncated     bool              `json:"truncated,omitempty"`
	Missing       bool              `json:"missing,omitempty"`
}

type ToolEvidence struct {
	Tool       string `json:"tool"`
	Status     string `json:"status,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	Result     string `json:"result,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type ConfirmationEvidence struct {
	Tool     string `json:"tool"`
	Status   string `json:"status"`
	ID       string `json:"id,omitempty"`
}

// ABResult is the per-case, per-run A/B outcome with loading and behavior evidence.
type ABResult struct {
	DatasetVersion string        `json:"dataset_version"`
	DatasetHash    string        `json:"dataset_hash"`
	Operation      string        `json:"operation"`
	SkillName      string        `json:"skill_name"`
	TargetAgent    string        `json:"target_agent"`
	Trials         int           `json:"trials"`
	Conclusion     string        `json:"conclusion"` // improved | not_improved | inconclusive
	Reasons        []string      `json:"reasons,omitempty"`
	Coverage       string        `json:"coverage,omitempty"` // sufficient | insufficient_coverage
	Metrics        Metrics       `json:"metrics"`
	Cases          []ABCaseResult `json:"cases"`
}

type Metrics struct {
	RequiredLoadRateA     float64 `json:"required_load_rate_a"`
	RequiredLoadRateB     float64 `json:"required_load_rate_b"`
	ForbiddenTriggerRateA float64 `json:"forbidden_trigger_rate_a"`
	ForbiddenTriggerRateB float64 `json:"forbidden_trigger_rate_b"`
	BehaviorPassRateA     float64 `json:"behavior_pass_rate_a"`
	BehaviorPassRateB     float64 `json:"behavior_pass_rate_b"`
	SafetyViolationsA     int     `json:"safety_violations_a"`
	SafetyViolationsB     int     `json:"safety_violations_b"`
	SemanticScoreA        string  `json:"semantic_score_a"` // pass | fail | unknown
	SemanticScoreB        string  `json:"semantic_score_b"`
	AvgToolCallsA         float64 `json:"avg_tool_calls_a"`
	AvgToolCallsB         float64 `json:"avg_tool_calls_b"`
}

type ABCaseResult struct {
	CaseID           string      `json:"case_id"`
	Capability       string      `json:"capability"`
	Category         string      `json:"category"`
	ExpectedSkillUse string      `json:"expected_skill_use"`
	GroupA           GroupResult `json:"group_a"`
	GroupB           GroupResult `json:"group_b"`
	Improved         bool        `json:"improved"`
	Regressed        bool        `json:"regressed"`
}

type GroupResult struct {
	Runs             int         `json:"runs"`
	Loads            int         `json:"loads"`
	BehaviorPass     int         `json:"behavior_pass"`
	BehaviorTotal    int         `json:"behavior_total"`
	SafetyViolations int         `json:"safety_violations"`
	Judge            string      `json:"judge"` // pass | fail | unknown
	AvgToolCalls     float64     `json:"avg_tool_calls"`
	AvgLatencyMS     float64     `json:"avg_latency_ms"`
	RunDetails       []RunDetail `json:"run_details,omitempty"`

	// unexported accumulators
	toolCallsTotal int
	latencyTotal   int64
}

type RunDetail struct {
	SkillLoaded   bool          `json:"skill_loaded"`
	SkillHash     string        `json:"skill_hash,omitempty"`
	Checks        []CheckResult `json:"checks,omitempty"`
	Violations    []string      `json:"violations,omitempty"`
	AssistantText string        `json:"assistant_text,omitempty"`
	ToolCalls     int           `json:"tool_calls,omitempty"`
	LatencyMS     int64         `json:"latency_ms,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type CheckResult struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}
