//go:build ai_skill_evolution

package evolution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

func TestLoadDirConfiguresSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "issue-triage", "覆盖版诊断", "覆盖版正文")
	writeSkill(t, dir, "new-skill", "新增能力", "新增正文")
	docs, err := skills.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %d, want 2", len(docs))
	}
	if docs["issue-triage"].Content != "覆盖版正文" {
		t.Fatalf("issue-triage content = %q", docs["issue-triage"].Content)
	}
	if docs["new-skill"].Description != "新增能力" {
		t.Fatalf("new-skill missing")
	}
}

func TestLoadDirRejectsNameMismatch(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "issue-triage")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: other-name\ndescription: 描述\n---\n\n正文"
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.LoadDir(dir); err == nil {
		t.Fatal("expected name mismatch error")
	}
}

func writeSkill(t *testing.T, dir, name, description, content string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + content
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFixtureFromTrace(t *testing.T) {
	session := SessionEvidence{
		TraceID: "t1",
		ToolCalls: []ToolEvidence{
			{Tool: "product_search", Result: `{"products":[{"product_id":101,"name":"A手机","price":199900,"category":"手机"}],"total":1}`},
			{Tool: "inventory_get", Result: `{"product_id":101,"inventory":5}`},
			{Tool: "order_get", Result: `{"order":{"order_id":"O123","user_id":7,"status":"pending_payment"}}`},
		},
	}
	fixture, err := buildFixtureFromTrace(session)
	if err != nil {
		t.Fatalf("buildFixtureFromTrace() error = %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(fixture, &state); err != nil {
		t.Fatal(err)
	}
	if products := state["products"].(map[string]any); products["101"] == nil {
		t.Fatalf("product 101 not rebuilt: %v", products)
	}
	if inventory := state["inventory"].(map[string]any); inventory["101"] != float64(5) {
		t.Fatalf("inventory not rebuilt: %v", inventory)
	}
	if orders := state["orders"].(map[string]any); orders["O123"] == nil {
		t.Fatalf("order O123 not rebuilt: %v", orders)
	}
}

func TestSameToolSetAndDifference(t *testing.T) {
	if !sameToolSet([]string{"order_get", "cart_add"}, []string{"cart_add", "order_get"}) {
		t.Fatal("set-equal tools should match")
	}
	if sameToolSet([]string{"order_get"}, []string{"order_get", "cart_add"}) {
		t.Fatal("different tools should not match")
	}
	diff := setDifference([]string{"cart_add", "order_cancel"}, []string{"order_cancel"})
	if len(diff) != 1 || diff[0] != "cart_add" {
		t.Fatalf("setDifference = %v, want [cart_add]", diff)
	}
}

func TestRunShadowConsistentWithOriginal(t *testing.T) {
	session := SessionEvidence{
		TraceID: "t1",
		Input:   "帮我查订单 O123",
		ToolCalls: []ToolEvidence{
			{Tool: "order_get", Result: `{"order":{"order_id":"O123","user_id":7,"status":"pending_payment"}}`},
		},
	}
	// Supervisor 委派 order_fulfillment_agent，子 Agent 内部调用 order_get。
	scripts := map[string][]*schema.Message{
		"supervisor_agent":         {toolCall("order_fulfillment_agent", `{"request":"查订单 O123"}`), reply("订单 O123 当前待支付")},
		"order_fulfillment_agent":  {toolCall("order_get", `{"order_id":"O123"}`), reply("订单 O123 当前待支付")},
	}
	summary, err := RunShadow(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, []SessionEvidence{session}, eino.SkillSet{})
	if err != nil {
		t.Fatalf("RunShadow() error = %v", err)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(summary.Results))
	}
	r := summary.Results[0]
	if !r.ToolsMatch {
		t.Fatalf("tools should match: original=%v shadow=%v", r.OriginalTools, r.ShadowTools)
	}
	if len(r.ExtraShadowWrites) != 0 || len(r.MissingShadowWrites) != 0 {
		t.Fatalf("unexpected write diff: extra=%v missing=%v", r.ExtraShadowWrites, r.MissingShadowWrites)
	}
	if summary.SideEffectRisk != 0 {
		t.Fatalf("side effect risk = %d, want 0", summary.SideEffectRisk)
	}
}

func TestRunShadowDetectsExtraWrite(t *testing.T) {
	session := SessionEvidence{
		TraceID: "t1",
		Input:   "查一下商品 101",
		ToolCalls: []ToolEvidence{
			{Tool: "product_detail", Result: `{"product":{"product_id":101,"name":"A手机","price":199900}}`},
		},
	}
	// 影子 Agent 在原轨迹之外多做了一个写操作 cart_add（副作用风险）。
	scripts := map[string][]*schema.Message{
		"supervisor_agent":         {toolCall("shopping_decision_agent", `{"request":"加购商品 101"}`), reply("已加入购物车")},
		"shopping_decision_agent":  {toolCall("cart_add", `{"product_id":101,"quantity":1}`), reply("已加入购物车")},
	}
	summary, err := RunShadow(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, []SessionEvidence{session}, eino.SkillSet{})
	if err != nil {
		t.Fatalf("RunShadow() error = %v", err)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(summary.Results))
	}
	r := summary.Results[0]
	if r.ToolsMatch {
		t.Fatal("tools should mismatch because shadow adds a write")
	}
	if len(r.ExtraShadowWrites) == 0 {
		t.Fatalf("expected extra shadow write, got shadow=%v", r.ShadowWrites)
	}
	if summary.SideEffectRisk != 1 {
		t.Fatalf("side effect risk = %d, want 1", summary.SideEffectRisk)
	}
}

func TestRunShadowSkipsMissingEvidence(t *testing.T) {
	session := SessionEvidence{TraceID: "t1", Missing: true}
	summary, err := RunShadow(context.Background(), ABConfig{
		TestedFactory: scriptedFactory(nil),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, []SessionEvidence{session}, eino.SkillSet{})
	if err != nil {
		t.Fatalf("RunShadow() error = %v", err)
	}
	if len(summary.Results) != 0 || summary.Total != 1 {
		t.Fatalf("missing evidence should be skipped: total=%d results=%d", summary.Total, len(summary.Results))
	}
}

func TestInferTraceOutcome(t *testing.T) {
	cases := []struct {
		calls []ToolEvidence
		want  string
	}{
		{[]ToolEvidence{{Tool: "order_cancel", Outcome: "executed"}}, "success"},
		{[]ToolEvidence{{Tool: "order_cancel", Outcome: "not_executed"}}, "failed"},
		{[]ToolEvidence{{Tool: "order_cancel", Outcome: "unknown"}}, "unknown"},
		{[]ToolEvidence{{Tool: "order_get", Status: "failed"}}, "failed"},
		{[]ToolEvidence{{Tool: "order_get", Status: "success"}}, "success"},
		{nil, "unknown"},
	}
	for _, c := range cases {
		if got := inferTraceOutcome(c.calls); got != c.want {
			t.Fatalf("inferTraceOutcome(%+v) = %q, want %q", c.calls, got, c.want)
		}
	}
}

func TestAsksClarification(t *testing.T) {
	if !asksClarification("请问您想要哪种商品？") {
		t.Fatal("clarification question should be detected")
	}
	if asksClarification("已为您查询到订单 O123，当前待支付。") {
		t.Fatal("plain statement should not be detected as clarification")
	}
}

func TestShadowEffectivenessSignals(t *testing.T) {
	// 原轨迹：写操作失败（结果未达成）且多了一次查询；影子：直接成功且工具更少。
	session := SessionEvidence{
		TraceID: "t1",
		Input:   "加购商品 101",
		ToolCalls: []ToolEvidence{
			{Tool: "product_detail", Status: "success", Result: `{"product":{"product_id":101,"name":"A手机","price":199900}}`},
			{Tool: "cart_add", Status: "failed", Outcome: "not_executed"},
		},
	}
	scripts := map[string][]*schema.Message{
		"supervisor_agent":        {toolCall("shopping_decision_agent", `{"request":"加购商品 101"}`), reply("已加入购物车")},
		"shopping_decision_agent": {toolCall("cart_add", `{"product_id":101,"quantity":1}`), reply("已加入购物车")},
	}
	summary, err := RunShadow(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, []SessionEvidence{session}, eino.SkillSet{})
	if err != nil {
		t.Fatalf("RunShadow() error = %v", err)
	}
	r := summary.Results[0]
	if r.OriginalOutcome != "failed" {
		t.Fatalf("original outcome = %q, want failed", r.OriginalOutcome)
	}
	if r.ShadowOutcome != "success" {
		t.Fatalf("shadow outcome = %q, want success", r.ShadowOutcome)
	}
	if !r.OutcomeImproved {
		t.Fatal("expected outcome improved (original failed → shadow success)")
	}
	if r.ShadowToolCount >= r.OriginalToolCount {
		t.Fatalf("expected fewer tools: original=%d shadow=%d, originalTools=%v shadowTools=%v", r.OriginalToolCount, r.ShadowToolCount, r.OriginalTools, r.ShadowTools)
	}
	if summary.OutcomeImproved != 1 {
		t.Fatalf("summary outcome improved = %d, want 1", summary.OutcomeImproved)
	}
}

func TestShadowToolCountReduced(t *testing.T) {
	// 原轨迹成功但多了一次查询；影子结果不变（也成功）但工具更少。
	session := SessionEvidence{
		TraceID: "t1",
		Input:   "加购商品 101",
		ToolCalls: []ToolEvidence{
			{Tool: "product_detail", Status: "success", Result: `{"product":{"product_id":101,"name":"A手机","price":199900}}`},
			{Tool: "cart_add", Status: "success", Outcome: "executed"},
		},
	}
	scripts := map[string][]*schema.Message{
		"supervisor_agent":        {toolCall("shopping_decision_agent", `{"request":"加购商品 101"}`), reply("已加入购物车")},
		"shopping_decision_agent": {toolCall("cart_add", `{"product_id":101,"quantity":1}`), reply("已加入购物车")},
	}
	summary, err := RunShadow(context.Background(), ABConfig{
		TestedFactory: scriptedFactoryWithAgents(scripts),
		TestedConfig:  config.EinoConfig{Provider: "openai", Model: "script"},
	}, []SessionEvidence{session}, eino.SkillSet{})
	if err != nil {
		t.Fatalf("RunShadow() error = %v", err)
	}
	r := summary.Results[0]
	if r.OriginalOutcome != "success" || r.ShadowOutcome != "success" {
		t.Fatalf("outcomes: original=%q shadow=%q, want success/success", r.OriginalOutcome, r.ShadowOutcome)
	}
	if r.OutcomeImproved {
		t.Fatal("result unchanged should not be marked outcome improved")
	}
	if !r.ToolCountReduced {
		t.Fatalf("expected tool count reduced: original=%d shadow=%d", r.OriginalToolCount, r.ShadowToolCount)
	}
	if summary.ToolReduced != 1 {
		t.Fatalf("summary tool reduced = %d, want 1", summary.ToolReduced)
	}
}
