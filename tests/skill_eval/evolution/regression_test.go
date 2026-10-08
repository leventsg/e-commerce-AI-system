//go:build ai_skill_evolution

package evolution

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

func TestLoadRegressionSet(t *testing.T) {
	set, hash, err := LoadRegressionSet(filepath.Join(repoRoot(t), "tests", "skill_eval", "regression.json"))
	if err != nil {
		t.Fatalf("LoadRegressionSet() error = %v", err)
	}
	if set.Version == "" || len(set.Cases) != 6 {
		t.Fatalf("set version=%q cases=%d, want 6 cases", set.Version, len(set.Cases))
	}
	if hash == "" {
		t.Fatal("hash should be non-empty")
	}
}

func TestValidateRegressionSetRejectsInvalid(t *testing.T) {
	if err := validateRegressionSet(RegressionSet{Version: "1", Cases: []RegressionCase{{ID: "c1"}}}); err == nil {
		t.Fatal("expected missing skill_name/input/expect_tool error")
	}
	if err := validateRegressionSet(RegressionSet{Version: "1"}); err == nil {
		t.Fatal("expected empty cases error")
	}
	if err := validateRegressionSet(RegressionSet{Cases: []RegressionCase{{ID: "c1", SkillName: "x", Input: "y", ExpectTool: "z"}}}); err == nil {
		t.Fatal("expected missing version error")
	}
}

func TestConcludeRegression(t *testing.T) {
	if got := concludeRegression(0, nil); got != "insufficient_coverage" {
		t.Fatalf("0 matched = %q, want insufficient_coverage", got)
	}
	if got := concludeRegression(2, []string{"c1"}); got != "regressed" {
		t.Fatalf("regressed = %q, want regressed", got)
	}
	if got := concludeRegression(2, nil); got != "not_regressed" {
		t.Fatalf("no regression = %q, want not_regressed", got)
	}
}

func TestRegressionRunPass(t *testing.T) {
	// supervisor 委派 order_fulfillment_agent，子 Agent 内部调 order_get。
	scripts := map[string][]*schema.Message{
		"supervisor_agent":        {toolCall("order_fulfillment_agent", `{"task":{"intent":"order_get","objective":"查订单 O123"},"inputs":{"order_id":"O123"}}`), reply("订单 O123 待支付")},
		"order_fulfillment_agent": {toolCall("order_get", `{"order_id":"O123"}`), childReply("订单 O123 待支付")},
	}
	c := RegressionCase{
		ID: "order_query", SkillName: "issue-triage", TargetAgent: "supervisor_agent",
		Input:      "帮我查订单 O123",
		Fixture:    json.RawMessage(`{"orders":{"O123":{"order_id":"O123","user_id":7,"status":"pending_payment","amount":199900}}}`),
		ExpectTool: "order_get",
	}
	if !regressionRun(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, c, eino.SkillSet{}, "t1") {
		t.Fatal("expected order_get call to pass")
	}
}

func TestRegressionRunMissingTool(t *testing.T) {
	// 模型没有调用期望的核心工具（调了别的），应判不通过。
	scripts := map[string][]*schema.Message{
		"supervisor_agent":        {toolCall("general_agent", `{"task":{"intent":"general_consultation","objective":"随便聊聊"}}`), reply("闲聊")},
	}
	c := RegressionCase{
		ID: "order_query", SkillName: "issue-triage", TargetAgent: "supervisor_agent",
		Input:      "帮我查订单 O123",
		Fixture:    json.RawMessage(`{"orders":{"O123":{"order_id":"O123","user_id":7,"status":"pending_payment"}}}`),
		ExpectTool: "order_get",
	}
	if regressionRun(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, c, eino.SkillSet{}, "t1") {
		t.Fatal("expected missing expect_tool to fail")
	}
}

func TestRunRegressionNotRegressed(t *testing.T) {
	// A/B 都用同一脚本调 order_get，均通过 → not_regressed。
	scripts := map[string][]*schema.Message{
		"supervisor_agent":        {toolCall("order_fulfillment_agent", `{"task":{"intent":"order_get","objective":"查订单 O123"},"inputs":{"order_id":"O123"}}`), reply("订单 O123 待支付")},
		"order_fulfillment_agent": {toolCall("order_get", `{"order_id":"O123"}`), childReply("订单 O123 待支付")},
	}
	set := &RegressionSet{Version: "1", Cases: []RegressionCase{{
		ID: "order_query", SkillName: "issue-triage", TargetAgent: "supervisor_agent",
		Input: "帮我查订单 O123", Fixture: json.RawMessage(`{"orders":{"O123":{"order_id":"O123","user_id":7,"status":"pending_payment"}}}`), ExpectTool: "order_get",
	}}}
	candidate := Candidate{
		Operation: "modify", SkillName: "issue-triage", TargetAgent: "supervisor_agent",
		SkillMarkdown: "---\nname: issue-triage\ndescription: 诊断\n---\n\n# 诊断\n\n正文",
	}
	report, err := RunRegression(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, set, "hash", candidate, 2)
	if err != nil {
		t.Fatalf("RunRegression() error = %v", err)
	}
	if report.Conclusion != "not_regressed" {
		t.Fatalf("conclusion = %q, want not_regressed (cases=%+v)", report.Conclusion, report.Cases)
	}
	if report.MatchedCases != 1 {
		t.Fatalf("matched = %d, want 1", report.MatchedCases)
	}
}

func TestRunRegressionInsufficientCoverage(t *testing.T) {
	// 回归集里没有候选 skill（issue-triage）的用例 → insufficient_coverage。
	set := &RegressionSet{Version: "1", Cases: []RegressionCase{{
		ID: "advisor_case", SkillName: "purchase-advisor", TargetAgent: "shopping_decision_agent",
		Input: "帮我挑手机", Fixture: json.RawMessage(`{"products":{}}`), ExpectTool: "product_search",
	}}}
	candidate := Candidate{
		Operation: "modify", SkillName: "issue-triage", TargetAgent: "supervisor_agent",
		SkillMarkdown: "---\nname: issue-triage\ndescription: 诊断\n---\n\n# 诊断\n\n正文",
	}
	report, err := RunRegression(context.Background(), ABConfig{
		TestedFactory: scriptedFactory(nil),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, set, "hash", candidate, 1)
	if err != nil {
		t.Fatalf("RunRegression() error = %v", err)
	}
	if report.Conclusion != "insufficient_coverage" {
		t.Fatalf("conclusion = %q, want insufficient_coverage", report.Conclusion)
	}
}

func childReply(summary string) *schema.Message {
    raw, err := json.Marshal(map[string]any{"status":"completed", "summary":summary, "evidence_refs":[]string{}, "actions":[]any{}, "missing_inputs":[]any{}, "remaining_work":[]string{}})
    if err != nil { panic(err) }
    return reply(string(raw))
}
