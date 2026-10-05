//go:build ai_business_agents

package eino

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

func TestSkillMiddlewareBindsIssueTriageToSupervisor(t *testing.T) {
	middleware, err := newSkillMiddleware(context.Background(), supervisorAgentName, nil)
	if err != nil {
		t.Fatalf("newSkillMiddleware() error = %v", err)
	}
	if middleware == nil {
		t.Fatal("supervisor skill middleware is nil")
	}
	runCtx := &adk.ChatModelAgentContext{Instruction: "基础指令"}
	_, next, err := middleware.BeforeAgent(context.Background(), runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent() error = %v", err)
	}
	if !strings.Contains(strings.ToLower(next.Instruction), "skill") {
		t.Fatalf("instruction missing skill system prompt: %q", next.Instruction)
	}
	if len(next.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1 skill tool", len(next.Tools))
	}
	info, err := next.Tools[0].Info(context.Background())
	if err != nil {
		t.Fatalf("skill tool Info() error = %v", err)
	}
	if info.Name != "skill" {
		t.Fatalf("skill tool name = %q", info.Name)
	}
	if !strings.Contains(info.Desc, "issue-triage") {
		t.Fatalf("skill tool desc missing issue-triage: %q", info.Desc)
	}
}

func TestSkillMiddlewareBindsPurchaseAdvisorToShoppingDecisionAgent(t *testing.T) {
	middleware, err := newSkillMiddleware(context.Background(), "shopping_decision_agent", nil)
	if err != nil {
		t.Fatalf("newSkillMiddleware() error = %v", err)
	}
	if middleware == nil {
		t.Fatal("shopping decision skill middleware is nil")
	}
	runCtx := &adk.ChatModelAgentContext{}
	_, next, err := middleware.BeforeAgent(context.Background(), runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent() error = %v", err)
	}
	info, err := next.Tools[0].Info(context.Background())
	if err != nil {
		t.Fatalf("skill tool Info() error = %v", err)
	}
	if !strings.Contains(info.Desc, "purchase-advisor") {
		t.Fatalf("skill tool desc missing purchase-advisor: %q", info.Desc)
	}
}

func TestSkillMiddlewareUnboundAgentReturnsNil(t *testing.T) {
	for _, agentName := range []string{"product_discovery_agent", "order_fulfillment_agent", "after_sales_agent", "general_agent"} {
		middleware, err := newSkillMiddleware(context.Background(), agentName, nil)
		if err != nil {
			t.Fatalf("newSkillMiddleware(%s) error = %v", agentName, err)
		}
		if middleware != nil {
			t.Fatalf("newSkillMiddleware(%s) should be nil", agentName)
		}
	}
}

func TestSkillOverrideReplacesDocumentAndRecordsLoad(t *testing.T) {
	ctx := context.Background()
	override := &skillOverride{
		set: SkillSet{Documents: map[string]skills.Document{
			skills.IssueTriage: {Name: skills.IssueTriage, Description: "候选版描述", Content: "候选版正文"},
		}},
	}
	loads := []SkillLoad{}
	override.onGet = func(name, hash string) { loads = append(loads, SkillLoad{Name: name, Hash: hash}) }
	middleware, err := newSkillMiddleware(ctx, supervisorAgentName, override)
	if err != nil {
		t.Fatalf("newSkillMiddleware() error = %v", err)
	}
	runCtx := &adk.ChatModelAgentContext{Instruction: "基础指令"}
	_, next, err := middleware.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent() error = %v", err)
	}
	// 通过 skill tool 的 Info 确认候选 description 生效。
	info, err := next.Tools[0].Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Desc, "issue-triage") {
		t.Fatalf("skill tool desc missing issue-triage: %q", info.Desc)
	}
	// 直接调用 backend.Get 触发真实加载记录。
	backend := &skillBackend{documents: []skills.Document{{Name: skills.IssueTriage, Description: "候选版描述", Content: "候选版正文"}}, onGet: override.onGet}
	if _, err := backend.Get(ctx, skills.IssueTriage); err != nil {
		t.Fatal(err)
	}
	if len(loads) != 1 || loads[0].Name != skills.IssueTriage || loads[0].Hash == "" {
		t.Fatalf("load not recorded: %+v", loads)
	}
}

func TestSkillSetAddNewSkillBinding(t *testing.T) {
	ctx := context.Background()
	override := &skillOverride{set: SkillSet{
		Bindings: map[string][]string{supervisorAgentName: {skills.IssueTriage, "new-skill"}},
		Documents: map[string]skills.Document{
			"new-skill": {Name: "new-skill", Description: "新增能力", Content: "新增正文"},
		},
	}}
	middleware, err := newSkillMiddleware(ctx, supervisorAgentName, override)
	if err != nil {
		t.Fatalf("newSkillMiddleware() error = %v", err)
	}
	_, next, err := middleware.BeforeAgent(ctx, &adk.ChatModelAgentContext{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := next.Tools[0].Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Desc, "new-skill") || !strings.Contains(info.Desc, "issue-triage") {
		t.Fatalf("skill tool desc missing new binding: %q", info.Desc)
	}
}

func TestWithSkillsSetsOverride(t *testing.T) {
	set := SkillSet{
		Bindings:  map[string][]string{supervisorAgentName: {skills.IssueTriage, "new-skill"}},
		Documents: map[string]skills.Document{"new-skill": {Name: "new-skill", Description: "新增能力", Content: "新增正文"}},
	}
	opts := &supervisorOptions{}
	WithSkills(set)(opts)
	if opts.skills == nil || opts.skills.set.Bindings[supervisorAgentName] == nil {
		t.Fatal("WithSkills did not set skill override")
	}
	if _, ok := opts.skills.set.Documents["new-skill"]; !ok {
		t.Fatal("WithSkills did not set skill documents")
	}
}
