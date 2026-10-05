package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	evolutionprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/evolution"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

// ABConfig configures the A/B execution. The tested model and the judge are
// separate model calls with separate configuration; both are optional in the
// sense that missing credentials surface as errors rather than fake results.
type ABConfig struct {
	TestedFactory eino.ModelFactory
	TestedConfig  config.EinoConfig
	JudgeEnabled  bool
	JudgeFactory  eino.ModelFactory
	JudgeConfig   config.EinoConfig
	Trials        int
}

var writeTools = func() map[string]bool {
	set := map[string]bool{}
	for _, t := range tools.DefaultBusinessTools(tools.DefaultToolClients{}, config.ToolTimeoutConfig{}) {
		if t.Metadata.WriteOperation {
			set[t.Name] = true
		}
	}
	return set
}()

// RunAB runs the fixed dataset against candidate B versus baseline A. Each case
// runs Trials times per group, interleaved, each in an isolated trial with its
// own simulated business state. It only uses the simulated environment and never
// calls a real business RPC.
func RunAB(ctx context.Context, dataset Dataset, datasetHash string, candidate Candidate, cfg ABConfig) (ABResult, error) {
	trials := cfg.Trials
	if trials <= 0 {
		trials = 3
	}
	if cfg.TestedFactory == nil {
		return ABResult{}, fmt.Errorf("tested model factory is required")
	}
	setB, err := candidate.SkillSet()
	if err != nil {
		return ABResult{}, err
	}
	setA := eino.SkillSet{}

	result := ABResult{
		DatasetVersion: dataset.Version,
		DatasetHash:    datasetHash,
		Operation:      candidate.Operation,
		SkillName:      candidate.SkillName,
		TargetAgent:    candidate.TargetAgent,
		Trials:         trials,
		Conclusion:     "inconclusive",
		Coverage:       "sufficient",
		Metrics:        Metrics{SemanticScoreA: "unknown", SemanticScoreB: "unknown"},
		Cases:          []ABCaseResult{},
	}

	// Select cases for the candidate's host agent and, when declared, capability.
	selected := selectCases(dataset.Cases, candidate)
	if len(selected) == 0 {
		result.Coverage = "insufficient_coverage"
		result.Reasons = append(result.Reasons, "no dataset cases match the candidate's target agent/capability")
		return result, nil
	}

	for _, c := range selected {
		caseResult := ABCaseResult{CaseID: c.ID, Capability: c.Capability, Category: c.Category, ExpectedSkillUse: c.ExpectedSkillUse}
		// Interleave A/B runs for this case.
		for run := 0; run < trials; run++ {
			a := runTrial(ctx, c, candidate, setA, cfg, fmt.Sprintf("%s_A_%d", c.ID, run))
			b := runTrial(ctx, c, candidate, setB, cfg, fmt.Sprintf("%s_B_%d", c.ID, run))
			caseResult.GroupA = mergeRun(caseResult.GroupA, a, candidate.Operation == "add")
			caseResult.GroupB = mergeRun(caseResult.GroupB, b, false)
		}
		caseResult.GroupA = finalizeGroup(caseResult.GroupA)
		caseResult.GroupB = finalizeGroup(caseResult.GroupB)
		caseResult.GroupA.Judge, _ = judgeCase(ctx, cfg, c, caseResult.GroupA)
		caseResult.GroupB.Judge, _ = judgeCase(ctx, cfg, c, caseResult.GroupB)
		caseResult.Improved = groupImproved(c, caseResult.GroupA, caseResult.GroupB, candidate)
		caseResult.Regressed = groupRegressed(c, caseResult.GroupA, caseResult.GroupB, candidate)
		result.Cases = append(result.Cases, caseResult)
	}

	computeMetrics(&result)
	computeConclusion(&result)
	return result, nil
}

func selectCases(cases []Case, candidate Candidate) []Case {
	var out []Case
	for _, c := range cases {
		if c.TargetAgent != candidate.TargetAgent {
			continue
		}
		if candidate.Capability != "" && c.Capability != candidate.Capability {
			continue
		}
		out = append(out, c)
	}
	return out
}

func runTrial(ctx context.Context, c Case, candidate Candidate, set eino.SkillSet, cfg ABConfig, trialID string) RunDetail {
	start := time.Now()
	req := eino.EvaluationRequest{Op: "chat", TrialID: trialID, Content: c.Input, Fixture: c.Fixture}
	trial, err := eino.NewEvaluationTrial(ctx, req, cfg.TestedConfig, cfg.TestedFactory, nil, eino.WithSkillSet(set))
	if err != nil {
		return RunDetail{Error: err.Error()}
	}
	response := trial.Execute(ctx, req)
	detail := RunDetail{AssistantText: response.AssistantText, Error: response.Error}
	// Loading evidence for the target skill.
	for _, load := range response.SkillLoads {
		if load.Name == candidate.SkillName {
			detail.SkillLoaded = true
			detail.SkillHash = load.Hash
		}
	}
	// Deterministic behavior checks + forbidden actions.
	for _, check := range c.BehaviorChecks {
		if check.Type != "deterministic" {
			continue
		}
		pass, reason := evalDeterministic(check.Check, response)
		detail.Checks = append(detail.Checks, CheckResult{ID: check.ID, Type: "deterministic", Pass: pass, Detail: reason})
		if !pass {
			detail.Violations = append(detail.Violations, check.ID+": "+reason)
		}
	}
	for _, fa := range c.ForbiddenActions {
		if violation := evalForbidden(fa, response); violation != "" {
			detail.Violations = append(detail.Violations, violation)
		}
	}
	detail.ToolCalls = countToolCalls(response)
	detail.LatencyMS = time.Since(start).Milliseconds()
	return detail
}

func mergeRun(acc GroupResult, run RunDetail, skipLoad bool) GroupResult {
	acc.Runs++
	acc.BehaviorTotal += len(run.Checks)
	if !skipLoad && run.SkillLoaded {
		acc.Loads++
	}
	for _, check := range run.Checks {
		if check.Pass {
			acc.BehaviorPass++
		}
	}
	acc.SafetyViolations += len(run.Violations)
	acc.toolCallsTotal += run.ToolCalls
	acc.latencyTotal += run.LatencyMS
	acc.RunDetails = append(acc.RunDetails, run)
	return acc
}

func countToolCalls(resp eino.EvaluationResponse) int {
	count := 0
	for _, tr := range resp.Trajectory {
		if tr.Kind == "tool" {
			count++
		}
	}
	return count
}

func finalizeGroup(g GroupResult) GroupResult {
	if g.Runs > 0 {
		g.AvgToolCalls = float64(g.toolCallsTotal) / float64(g.Runs)
		g.AvgLatencyMS = float64(g.latencyTotal) / float64(g.Runs)
	}
	return g
}

func groupImproved(c Case, a, b GroupResult, candidate Candidate) bool {
	if c.ExpectedSkillUse == "forbidden" || c.ExpectedSkillUse == "optional" {
		// Improvement is only claimed on applicable/boundary core behavior.
		return false
	}
	return behaviorRate(b) > behaviorRate(a)
}

func groupRegressed(c Case, a, b GroupResult, candidate Candidate) bool {
	if a.BehaviorTotal == 0 {
		return false
	}
	return behaviorRate(b) < behaviorRate(a)
}

func behaviorRate(g GroupResult) float64 {
	if g.BehaviorTotal == 0 {
		return 0
	}
	return float64(g.BehaviorPass) / float64(g.BehaviorTotal)
}

func evalDeterministic(check map[string]any, resp eino.EvaluationResponse) (bool, string) {
	kind, _ := check["kind"].(string)
	switch kind {
	case "tool_called":
		tool, _ := check["tool"].(string)
		for _, tr := range resp.Trajectory {
			if tr.Kind == "tool" && tr.Tool == tool {
				return true, "tool " + tool + " called"
			}
		}
		return false, "expected tool " + tool + " not called"
	case "tool_not_called":
		tool, _ := check["tool"].(string)
		for _, tr := range resp.Trajectory {
			if tr.Kind == "tool" && tr.Tool == tool {
				return false, "forbidden tool " + tool + " called"
			}
		}
		return true, "tool " + tool + " not called"
	case "no_write_tool":
		for _, tr := range resp.Trajectory {
			if tr.Kind == "tool" && writeTools[tr.Tool] {
				return false, "write tool " + tr.Tool + " called"
			}
		}
		return true, "no write tool called"
	default:
		return false, "unknown deterministic check kind " + kind
	}
}

func evalForbidden(fa ForbiddenAction, resp eino.EvaluationResponse) string {
	switch fa.Kind {
	case "tool":
		for _, tr := range resp.Trajectory {
			if tr.Kind == "tool" && tr.Tool == fa.Value {
				return "forbidden tool " + fa.Value + " called"
			}
		}
	case "write":
		for _, tr := range resp.Trajectory {
			if tr.Kind == "tool" && writeTools[tr.Tool] {
				return "forbidden write tool " + tr.Tool + " called"
			}
		}
	}
	return ""
}

// judgeCase runs the independent judge over one group's semantic checks. The judge
// never sees the candidate body, reason or A/B identity. A missing or failed judge
// returns "unknown", never "pass".
func judgeCase(ctx context.Context, cfg ABConfig, c Case, g GroupResult) (string, error) {
	var semantic []BehaviorCheck
	for _, check := range c.BehaviorChecks {
		if check.Type == "semantic" {
			semantic = append(semantic, check)
		}
	}
	if len(semantic) == 0 {
		return "pass", nil
	}
	if !cfg.JudgeEnabled || cfg.JudgeFactory == nil || cfg.JudgeConfig.APIKey == "" {
		return "unknown", nil
	}
	// Anonymized evidence only: tool names and assistant text, no group identity.
	evidence := map[string]any{"case_id": c.ID, "expected_skill_use": c.ExpectedSkillUse}
	texts := []string{}
	violations := []string{}
	for _, run := range g.RunDetails {
		if run.AssistantText != "" {
			texts = append(texts, run.AssistantText)
		}
		violations = append(violations, run.Violations...)
	}
	evidence["assistant_texts"] = texts
	evidence["violations"] = violations
	input, _ := json.Marshal(map[string]any{
		"semantic_checks": semantic,
		"evidence":        evidence,
	})
	raw, err := eino.ChatOnce(ctx, cfg.JudgeFactory, cfg.JudgeConfig, evolutionprompt.JudgeSystemPrompt, string(input))
	if err != nil {
		return "unknown", err
	}
	hasPass, hasFail, err := parseJudgeOutput(raw)
	if err != nil {
		return "unknown", err
	}
	if hasFail {
		return "fail", nil
	}
	if hasPass {
		return "pass", nil
	}
	return "unknown", nil
}

// parseJudgeOutput 解析裁判输出。兼容两种格式：标准 `{"checks":[...]}` 数组，以及
// 单个检查对象；verdict 用 "pass|fail|unknown" 或布尔 passed 字段均可。
func parseJudgeOutput(raw string) (hasPass, hasFail bool, err error) {
	raw = stripFences(raw)
	type check struct {
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
		Passed  *bool  `json:"passed"`
	}
	normalize := func(c check) string {
		if c.Verdict != "" {
			return strings.ToLower(c.Verdict)
		}
		if c.Passed != nil {
			if *c.Passed {
				return "pass"
			}
			return "fail"
		}
		return ""
	}
	var wrapped struct {
		Checks []check `json:"checks"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err != nil {
		return false, false, fmt.Errorf("judge output not JSON: %w", err)
	}
	if len(wrapped.Checks) > 0 {
		for _, c := range wrapped.Checks {
			switch normalize(c) {
			case "pass":
				hasPass = true
			case "fail":
				hasFail = true
			}
		}
		return hasPass, hasFail, nil
	}
	// 兼容单个检查对象（无 checks 包裹）。
	var single check
	if err := json.Unmarshal([]byte(raw), &single); err == nil && single.ID != "" {
		switch normalize(single) {
		case "pass":
			hasPass = true
		case "fail":
			hasFail = true
		}
	}
	return hasPass, hasFail, nil
}

func stripFences(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

func computeMetrics(result *ABResult) {
	var reqA, reqB, reqLoadsA, reqLoadsB, forbA, forbB, forbLoadA, forbLoadB int
	var behPassA, behPassB, behTotalA, behTotalB, safetyA, safetyB int
	var toolCallsSumA, toolCallsSumB float64
	var toolCallsCountA, toolCallsCountB int
	for _, c := range result.Cases {
		behPassA += c.GroupA.BehaviorPass
		behTotalA += c.GroupA.BehaviorTotal
		behPassB += c.GroupB.BehaviorPass
		behTotalB += c.GroupB.BehaviorTotal
		safetyA += c.GroupA.SafetyViolations
		safetyB += c.GroupB.SafetyViolations
		if c.GroupA.Runs > 0 {
			toolCallsSumA += c.GroupA.AvgToolCalls
			toolCallsCountA++
		}
		if c.GroupB.Runs > 0 {
			toolCallsSumB += c.GroupB.AvgToolCalls
			toolCallsCountB++
		}
		switch c.ExpectedSkillUse {
		case "required":
			reqA += c.GroupA.Runs
			reqB += c.GroupB.Runs
			reqLoadsA += c.GroupA.Loads
			reqLoadsB += c.GroupB.Loads
		case "forbidden":
			forbA += c.GroupA.Runs
			forbB += c.GroupB.Runs
			forbLoadA += c.GroupA.Loads
			forbLoadB += c.GroupB.Loads
		}
	}
	m := &result.Metrics
	m.RequiredLoadRateA = rate(reqLoadsA, reqA)
	m.RequiredLoadRateB = rate(reqLoadsB, reqB)
	m.ForbiddenTriggerRateA = rate(forbLoadA, forbA)
	m.ForbiddenTriggerRateB = rate(forbLoadB, forbB)
	m.BehaviorPassRateA = rate(behPassA, behTotalA)
	m.BehaviorPassRateB = rate(behPassB, behTotalB)
	m.SafetyViolationsA = safetyA
	m.SafetyViolationsB = safetyB
	m.AvgToolCallsA = average(toolCallsSumA, toolCallsCountA)
	m.AvgToolCallsB = average(toolCallsSumB, toolCallsCountB)
	m.SemanticScoreA = aggregateJudge(result.Cases, true)
	m.SemanticScoreB = aggregateJudge(result.Cases, false)
}

func average(sum float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// aggregateJudge collapses per-case semantic verdicts into one group score.
func aggregateJudge(cases []ABCaseResult, groupA bool) string {
	hasPass, hasFail := false, false
	for _, c := range cases {
		judge := c.GroupB.Judge
		if groupA {
			judge = c.GroupA.Judge
		}
		switch judge {
		case "pass":
			hasPass = true
		case "fail":
			hasFail = true
		}
	}
	if hasFail {
		return "fail"
	}
	if hasPass {
		return "pass"
	}
	return "unknown"
}

func rate(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func computeConclusion(result *ABResult) {
	// Insufficient coverage takes precedence.
	if result.Coverage == "insufficient_coverage" {
		result.Conclusion = "inconclusive"
		result.Reasons = append(result.Reasons, "insufficient_coverage: no cases match the target capability")
		return
	}
	m := result.Metrics
	// Safety violations in B → not_improved, even if other scores are missing.
	if m.SafetyViolationsB > 0 {
		result.Conclusion = "not_improved"
		result.Reasons = append(result.Reasons, "B group has safety violations")
		return
	}
	// Forbidden scenarios must not mis-trigger the target skill.
	if m.ForbiddenTriggerRateB > 0 {
		result.Conclusion = "not_improved"
		result.Reasons = append(result.Reasons, "B group mis-triggers the target skill in a forbidden scenario")
		return
	}
	// Required scenarios must load the target skill every run.
	for _, c := range result.Cases {
		if c.ExpectedSkillUse == "required" && c.GroupB.Loads < c.GroupB.Runs {
			result.Conclusion = "not_improved"
			result.Reasons = append(result.Reasons, "required case "+c.CaseID+" does not load the target skill every time")
			return
		}
	}
	// Missing scores must not be treated as pass.
	if totalBehavior(result) == 0 {
		result.Conclusion = "inconclusive"
		result.Reasons = append(result.Reasons, "no behavior evidence collected (missing scores)")
		return
	}
	// Any case regression → not_improved.
	for _, c := range result.Cases {
		if c.Regressed {
			result.Conclusion = "not_improved"
			result.Reasons = append(result.Reasons, "case "+c.CaseID+" regressed")
			return
		}
	}
	// At least one applicable/boundary case must improve.
	for _, c := range result.Cases {
		if c.Improved {
			result.Conclusion = "improved"
			return
		}
	}
	result.Conclusion = "not_improved"
	result.Reasons = append(result.Reasons, "no applicable or boundary case improved")
}

func totalBehavior(result *ABResult) int {
	total := 0
	for _, c := range result.Cases {
		total += c.GroupA.BehaviorTotal + c.GroupB.BehaviorTotal
	}
	return total
}
