//go:build ai_skill_evolution

package evolution

import (
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

const validModifyMarkdown = `---
name: issue-triage
description: 候选版：当用户描述含糊的电商操作异常时使用。
---

# 电商问题诊断（候选版）

先核查再解释，不调用任何写工具，信息不足时先澄清。`

func TestValidateCandidateModifyValid(t *testing.T) {
	hash, _ := eino.EmbeddedSkillHash("issue-triage")
	candidate := Candidate{
		Operation:         "modify",
		SkillName:         "issue-triage",
		TargetAgent:       "supervisor_agent",
		BaselineSkillHash: hash,
		SkillMarkdown:     validModifyMarkdown,
	}
	if err := ValidateCandidate(candidate); err != nil {
		t.Fatalf("ValidateCandidate(modify) = %v", err)
	}
}

func TestValidateCandidateModifyBaselineMismatch(t *testing.T) {
	candidate := Candidate{
		Operation:         "modify",
		SkillName:         "issue-triage",
		TargetAgent:       "supervisor_agent",
		BaselineSkillHash: "deadbeef",
		SkillMarkdown:     validModifyMarkdown,
	}
	if err := ValidateCandidate(candidate); err == nil {
		t.Fatal("expected baseline hash mismatch error")
	}
}

func TestValidateCandidateAddValid(t *testing.T) {
	candidate := Candidate{
		Operation:     "add",
		SkillName:     "refund-guide",
		TargetAgent:   "after_sales_agent",
		SkillMarkdown: "---\nname: refund-guide\ndescription: 退款政策咨询\n---\n\n# 退款政策\n\n仅说明政策，不调用写工具。",
		Bindings:      map[string][]string{"after_sales_agent": {"refund-guide"}},
	}
	if err := ValidateCandidate(candidate); err != nil {
		t.Fatalf("ValidateCandidate(add) = %v", err)
	}
}

func TestValidateCandidateAddMissingBindings(t *testing.T) {
	candidate := Candidate{
		Operation:     "add",
		SkillName:     "refund-guide",
		TargetAgent:   "after_sales_agent",
		SkillMarkdown: "---\nname: refund-guide\ndescription: 退款政策咨询\n---\n\n# 退款政策",
	}
	if err := ValidateCandidate(candidate); err == nil {
		t.Fatal("expected missing bindings error")
	}
}

func TestCandidateSkillSetModifyReplaces(t *testing.T) {
	candidate := Candidate{Operation: "modify", SkillName: "issue-triage", SkillMarkdown: validModifyMarkdown}
	set, err := candidate.SkillSet()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Bindings) != 0 {
		t.Fatalf("modify should not change bindings, got %v", set.Bindings)
	}
	doc, ok := set.Documents["issue-triage"]
	if !ok || doc.Name != "issue-triage" {
		t.Fatalf("modify should replace the target document, got %+v", set.Documents)
	}
}

func TestCandidateSkillSetAddAppends(t *testing.T) {
	candidate := Candidate{
		Operation:     "add",
		SkillName:     "refund-guide",
		SkillMarkdown: "---\nname: refund-guide\ndescription: 退款政策咨询\n---\n\n# 退款政策",
		Bindings:      map[string][]string{"after_sales_agent": {"refund-guide"}},
	}
	set, err := candidate.SkillSet()
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Bindings["after_sales_agent"]; len(got) != 1 || got[0] != "refund-guide" {
		t.Fatalf("add should append binding, got %v", set.Bindings)
	}
	if _, ok := set.Documents["refund-guide"]; !ok {
		t.Fatal("add should include the new document")
	}
}
