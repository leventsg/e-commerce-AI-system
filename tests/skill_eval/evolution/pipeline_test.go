//go:build ai_skill_evolution

package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

// jsonFactory returns a ModelFactory whose single non-tool model call returns the
// given fixed JSON text. It models the independent analysis/generation/judge calls
// deterministically; it never performs a business RPC.
func jsonFactory(jsonText string) eino.ModelFactory {
	return eino.NewModelFactory(eino.WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return &scriptedModel{script: []*schema.Message{reply(jsonText)}}, nil
	}))
}

func TestParseCandidateFencedAndNoCandidate(t *testing.T) {
	candidate, err := ParseCandidate("```json\n" + `{"operation":"modify","skill_name":"issue-triage","target_agent":"supervisor_agent","skill_markdown":"---\nname: issue-triage\ndescription: d\n---\n\n正文"}` + "\n```")
	if err != nil {
		t.Fatalf("ParseCandidate() error = %v", err)
	}
	if candidate == nil || candidate.Operation != "modify" {
		t.Fatalf("candidate = %+v", candidate)
	}
	none, err := ParseCandidate(`{"no_candidate": true, "reason": "无缺口"}`)
	if err != nil {
		t.Fatalf("ParseCandidate(no_candidate) error = %v", err)
	}
	if none != nil {
		t.Fatalf("no_candidate should yield nil, got %+v", none)
	}
}

func TestAnalyzeProductionWithScriptedModel(t *testing.T) {
	modelJSON := `{"sessions":[{"trace_id":"t1","session_id":"s1","conclusion":"failure","goal":"取消订单","skill_gap":true,"evidence":["order_get 失败"]}],"gap":"含糊异常缺少核查步骤","success_pattern":""}`
	sessions := []SessionEvidence{{
		TraceID: "t1", SessionID: "s1", Agent: "supervisor_agent",
		Input: "帮我取消订单", Output: "无法取消",
	}}
	out, err := AnalyzeProduction(context.Background(), jsonFactory(modelJSON), config.EinoConfig{Provider: "openai", Model: "script"}, sessions, "from", "to", "supervisor_agent", 20)
	if err != nil {
		t.Fatalf("AnalyzeProduction() error = %v", err)
	}
	if len(out.Sessions) == 0 || out.Sessions[0].Conclusion != "failure" {
		t.Fatalf("sessions = %+v", out.Sessions)
	}
	if !strings.Contains(out.Gap, "核查") {
		t.Fatalf("gap = %q", out.Gap)
	}
}

func TestAnalyzeProductionSkipsMissingEvidence(t *testing.T) {
	sessions := []SessionEvidence{
		{TraceID: "t1", Missing: true},
		{TraceID: "t2", Input: "   "},
		{TraceID: "t3", Input: "正常输入"},
	}
	out, err := AnalyzeProduction(context.Background(), jsonFactory(`{"sessions":[{"trace_id":"t3","conclusion":"success"}]}`), config.EinoConfig{Provider: "openai", Model: "script"}, sessions, "from", "to", "supervisor_agent", 20)
	if err != nil {
		t.Fatalf("AnalyzeProduction() error = %v", err)
	}
	if len(out.Skips) != 2 {
		t.Fatalf("skips = %d, want 2", len(out.Skips))
	}
}

func TestGenerateCandidateWithScriptedModel(t *testing.T) {
	modelJSON := `{"operation":"modify","skill_name":"issue-triage","target_agent":"supervisor_agent","skill_markdown":"---\nname: issue-triage\ndescription: 候选版诊断\n---\n\n# 诊断\n\n先核查再解释，不调用写工具。","reason":"补充核查步骤","source_trace_ids":["t1"]}`
	analysis := AnalysisOutput{TargetAgent: "supervisor_agent", Gap: "含糊异常缺少核查步骤"}
	candidate, err := GenerateCandidate(context.Background(), jsonFactory(modelJSON), config.EinoConfig{Provider: "openai", Model: "script"}, analysis)
	if err != nil {
		t.Fatalf("GenerateCandidate() error = %v", err)
	}
	if candidate == nil || candidate.Operation != "modify" || candidate.SkillName != "issue-triage" {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestGenerateCandidateNoCandidate(t *testing.T) {
	candidate, err := GenerateCandidate(context.Background(), jsonFactory(`{"no_candidate":true}`), config.EinoConfig{Provider: "openai", Model: "script"}, AnalysisOutput{})
	if err != nil {
		t.Fatalf("GenerateCandidate() error = %v", err)
	}
	if candidate != nil {
		t.Fatalf("expected nil candidate, got %+v", candidate)
	}
}
