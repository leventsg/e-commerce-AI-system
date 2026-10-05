// Package evolution implements the minimal Self-Evolving closed loop: read redacted
// production evidence, derive one Skill candidate from failure/detour experience,
// and replay the original traces to verify the candidate's effectiveness. It is a
// local, single-run command; it adds no database, worker, queue or admin page.
package evolution

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

