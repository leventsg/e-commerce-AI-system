//go:build ai_skill_evolution

package evolution

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

func TestRunABRecordsLoadsAndChecks(t *testing.T) {
	dataset := Dataset{Version: "test", Cases: []Case{{
		ID:               "req1",
		Capability:       "issue-triage",
		TargetAgent:      "supervisor_agent",
		SkillName:        "issue-triage",
		Category:         "applicable",
		ExpectedSkillUse: "required",
		Input:            "帮我看看刚才下单到底成功没有",
		Fixture:          json.RawMessage(`{"orders":{"O123":{"order_id":"O123","user_id":7,"status":"pending_payment"}}}`),
		BehaviorChecks:   []BehaviorCheck{{ID: "readonly", Type: "deterministic", Description: "不写", Check: map[string]any{"kind": "no_write_tool"}}},
	}}}

	supervisor := []*schema.Message{
		toolCall("skill", `{"skill":"issue-triage"}`),
		reply("已核查订单 O123，当前待支付，请不要重复下单"),
	}
	candidate := Candidate{
		Operation:         "modify",
		SkillName:         "issue-triage",
		TargetAgent:       "supervisor_agent",
		BaselineSkillHash: "",
		SkillMarkdown:     validModifyMarkdown,
	}

	result, err := RunAB(context.Background(), dataset, "testhash", candidate, ABConfig{
		TestedFactory: scriptedFactory(supervisor),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
		Trials:        3,
	})
	if err != nil {
		t.Fatalf("RunAB() error = %v", err)
	}
	if len(result.Cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(result.Cases))
	}
	got := result.Cases[0]
	if got.GroupB.Loads != 3 {
		t.Fatalf("B loads = %d, want 3", got.GroupB.Loads)
	}
	if got.GroupA.Loads != 3 {
		t.Fatalf("A loads = %d, want 3 (modify keeps original load)", got.GroupA.Loads)
	}
	// Hashes must differ: A loads the embedded skill, B the candidate skill.
	if len(got.GroupA.RunDetails) == 0 || len(got.GroupB.RunDetails) == 0 {
		t.Fatal("missing run details")
	}
	if got.GroupA.RunDetails[0].SkillHash == got.GroupB.RunDetails[0].SkillHash {
		t.Fatalf("A/B skill hashes should differ: %s", got.GroupA.RunDetails[0].SkillHash)
	}
	embeddedHash, _ := eino.EmbeddedSkillHash("issue-triage")
	if got.GroupA.RunDetails[0].SkillHash != embeddedHash {
		t.Fatalf("A hash = %s, want embedded %s", got.GroupA.RunDetails[0].SkillHash, embeddedHash)
	}
	candidateDoc, _ := skills.Parse(validModifyMarkdown)
	if got.GroupB.RunDetails[0].SkillHash != skills.Hash(candidateDoc) {
		t.Fatalf("B hash = %s, want candidate %s", got.GroupB.RunDetails[0].SkillHash, skills.Hash(candidateDoc))
	}
	// Behavior: no_write_tool passes for both groups.
	if got.GroupA.BehaviorPass != 3 || got.GroupB.BehaviorPass != 3 {
		t.Fatalf("behavior pass A=%d B=%d, want 3/3", got.GroupA.BehaviorPass, got.GroupB.BehaviorPass)
	}
	// Identical behavior ⇒ no improvement.
	if result.Conclusion != "not_improved" {
		t.Fatalf("conclusion = %s, want not_improved (reasons=%v)", result.Conclusion, result.Reasons)
	}
}

func TestRunABInsufficientCoverage(t *testing.T) {
	dataset := Dataset{Version: "test", Cases: []Case{{
		ID: "other", Capability: "purchase-advisor", TargetAgent: "shopping_decision_agent", SkillName: "purchase-advisor",
		Category: "applicable", ExpectedSkillUse: "required", Input: "帮我选手机",
		Fixture:        json.RawMessage(`{}`),
		BehaviorChecks: []BehaviorCheck{{ID: "r", Type: "deterministic", Check: map[string]any{"kind": "no_write_tool"}}},
	}}}
	candidate := Candidate{Operation: "modify", SkillName: "issue-triage", TargetAgent: "supervisor_agent", SkillMarkdown: validModifyMarkdown}
	result, err := RunAB(context.Background(), dataset, "hash", candidate, ABConfig{
		TestedFactory: scriptedFactory(nil), TestedConfig: config.EinoConfig{Provider: "openai", Model: "script"},
	})
	if err != nil {
		t.Fatalf("RunAB() error = %v", err)
	}
	if result.Coverage != "insufficient_coverage" || result.Conclusion != "inconclusive" {
		t.Fatalf("coverage=%s conclusion=%s, want insufficient_coverage/inconclusive", result.Coverage, result.Conclusion)
	}
}

func TestComputeConclusionImproved(t *testing.T) {
	result := &ABResult{Coverage: "sufficient", Cases: []ABCaseResult{{
		CaseID: "req1", Category: "applicable", ExpectedSkillUse: "required",
		GroupA: GroupResult{Runs: 3, Loads: 3, BehaviorPass: 1, BehaviorTotal: 3},
		GroupB: GroupResult{Runs: 3, Loads: 3, BehaviorPass: 3, BehaviorTotal: 3},
		Improved: true,
	}}}
	computeMetrics(result)
	computeConclusion(result)
	if result.Conclusion != "improved" {
		t.Fatalf("conclusion = %s, want improved (reasons=%v)", result.Conclusion, result.Reasons)
	}
}

func TestComputeConclusionSafetyViolationTakesPrecedence(t *testing.T) {
	result := &ABResult{Coverage: "sufficient", Cases: []ABCaseResult{{
		CaseID: "req1", Category: "applicable", ExpectedSkillUse: "required",
		GroupA: GroupResult{Runs: 3, Loads: 3, BehaviorPass: 3, BehaviorTotal: 3},
		GroupB: GroupResult{Runs: 3, Loads: 3, BehaviorPass: 3, BehaviorTotal: 3, SafetyViolations: 1},
	}}}
	computeMetrics(result)
	computeConclusion(result)
	if result.Conclusion != "not_improved" {
		t.Fatalf("conclusion = %s, want not_improved (reasons=%v)", result.Conclusion, result.Reasons)
	}
}

func TestComputeConclusionRequiredNotLoaded(t *testing.T) {
	result := &ABResult{Coverage: "sufficient", Cases: []ABCaseResult{{
		CaseID: "req1", Category: "applicable", ExpectedSkillUse: "required",
		GroupA: GroupResult{Runs: 3, Loads: 3, BehaviorPass: 3, BehaviorTotal: 3},
		GroupB: GroupResult{Runs: 3, Loads: 1, BehaviorPass: 3, BehaviorTotal: 3},
	}}}
	computeMetrics(result)
	computeConclusion(result)
	if result.Conclusion != "not_improved" {
		t.Fatalf("conclusion = %s, want not_improved (reasons=%v)", result.Conclusion, result.Reasons)
	}
}
