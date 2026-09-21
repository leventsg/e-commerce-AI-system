package eino

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
)

func TestSkillMiddlewareBindsIssueTriageToSupervisor(t *testing.T) {
	middleware, err := newSkillMiddleware(context.Background(), supervisorAgentName)
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

func TestSkillMiddlewareBindsPurchaseAdvisorToProductAgent(t *testing.T) {
	middleware, err := newSkillMiddleware(context.Background(), "product_agent")
	if err != nil {
		t.Fatalf("newSkillMiddleware() error = %v", err)
	}
	if middleware == nil {
		t.Fatal("product skill middleware is nil")
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
	for _, agentName := range []string{"order_agent", "cart_checkout_agent", "coupon_agent", "user_agent", "general_agent"} {
		middleware, err := newSkillMiddleware(context.Background(), agentName)
		if err != nil {
			t.Fatalf("newSkillMiddleware(%s) error = %v", agentName, err)
		}
		if middleware != nil {
			t.Fatalf("newSkillMiddleware(%s) should be nil", agentName)
		}
	}
}
