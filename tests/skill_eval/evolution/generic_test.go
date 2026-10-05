//go:build ai_skill_evolution

package evolution

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

func TestIsReadOnlySkill(t *testing.T) {
	for _, s := range []string{"本 Skill 不调用任何写工具", "只读工具", "不调用 checkout_prepare", "不通过子 Agent 间接执行写操作"} {
		if !isReadOnlySkill(s) {
			t.Fatalf("isReadOnlySkill(%q) = false, want true", s)
		}
	}
	if isReadOnlySkill("可以调用任何工具完成操作") {
		t.Fatal("expected not read-only")
	}
}

func TestApplicableSample(t *testing.T) {
	if got := applicableSample("当用户描述含糊异常时使用。例如优惠券明明存在却无法使用、下单无响应。"); got != "优惠券明明存在却无法使用" {
		t.Fatalf("applicableSample = %q", got)
	}
	if got := applicableSample("适用于给父母选手机、挑礼物等复杂选购咨询。"); got != "给父母选手机" {
		t.Fatalf("applicableSample = %q", got)
	}
}

func TestExtractProfile(t *testing.T) {
	c := Candidate{
		Operation: "modify", SkillName: "issue-triage", TargetAgent: "supervisor_agent",
		SkillMarkdown: "---\nname: issue-triage\ndescription: 诊断\n---\n\n# 诊断\n\n本 Skill 不调用任何写工具。",
	}
	p, err := extractProfile(c)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "issue-triage" || !p.ReadOnly {
		t.Fatalf("profile = %+v", p)
	}
}

func TestGenerateGenericCases(t *testing.T) {
	c := Candidate{Operation: "modify", SkillName: "issue-triage", TargetAgent: "supervisor_agent", Capability: "issue-triage"}
	p := SkillProfile{Name: "issue-triage", Description: "当用户描述含糊异常时使用。例如优惠券明明存在却无法使用。", Content: "不调用任何写工具", ReadOnly: true}
	cases := generateGenericCases(c, p)
	if len(cases) != 6 {
		t.Fatalf("cases = %d, want 6", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if len(c.BehaviorChecks) == 0 {
			t.Fatalf("case %s has no behavior_checks", c.ID)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate case id %s", c.ID)
		}
		seen[c.ID] = true
	}
	for _, id := range []string{
		"generic_trigger_applicable", "generic_trigger_not_applicable",
		"generic_safety_injection", "generic_safety_overreach",
		"generic_robustness_insufficient", "generic_robustness_out_of_scope",
	} {
		if !seen[id] {
			t.Fatalf("missing case %s", id)
		}
	}
}

func TestGenericEvalRunsFourLayers(t *testing.T) {
	hash, _ := eino.EmbeddedSkillHash("issue-triage")
	c := Candidate{
		Operation:         "modify",
		SkillName:         "issue-triage",
		TargetAgent:       "supervisor_agent",
		Capability:        "issue-triage",
		BaselineSkillHash: hash,
		SkillMarkdown:     "---\nname: issue-triage\ndescription: 当用户描述含糊的电商操作异常时使用。例如优惠券明明存在却无法使用。\n---\n\n# 电商问题诊断\n\n先核查再解释，不调用任何写工具，信息不足时先澄清。",
	}
	report, err := GenericEval(context.Background(), c, ABConfig{
		TestedFactory: scriptedFactory([]*schema.Message{reply("已处理")}),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, 1)
	if err != nil {
		t.Fatalf("GenericEval() error = %v", err)
	}
	if report.SkillName != "issue-triage" || !report.ReadOnly {
		t.Fatalf("report header = %+v", report)
	}
	if report.ApplicableSample == "" {
		t.Fatal("applicable sample should be extracted")
	}
	if report.Conclusion == "" {
		t.Fatal("conclusion should be set")
	}
	switch report.Conclusion {
	case "improved", "not_improved", "inconclusive":
	default:
		t.Fatalf("invalid conclusion %q", report.Conclusion)
	}
}
