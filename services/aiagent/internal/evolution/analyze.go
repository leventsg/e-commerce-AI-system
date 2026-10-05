package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	evolutionprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/evolution"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

// AnalyzeProduction runs the independent analysis over redacted production
// sessions. It only sees redacted evidence and real business outcomes; it never
// reads the skill eval dataset or any scoring standard.
func AnalyzeProduction(ctx context.Context, factory eino.ModelFactory, cfg config.EinoConfig, sessions []SessionEvidence, windowFrom, windowTo, agent string, maxSessions int) (AnalysisOutput, error) {
	out := AnalysisOutput{WindowFrom: windowFrom, WindowTo: windowTo, TargetAgent: agent, MaxSessions: maxSessions}
	// Skip sessions without usable evidence; never fabricate content.
	usable := make([]SessionEvidence, 0, len(sessions))
	for _, s := range sessions {
		if s.Missing || s.Truncated {
			out.Skips = append(out.Skips, SessionAnalysis{TraceID: s.TraceID, SessionID: s.SessionID, Agent: s.Agent, Conclusion: "unknown", SkipReason: "missing or truncated evidence"})
			continue
		}
		if strings.TrimSpace(s.Input) == "" {
			out.Skips = append(out.Skips, SessionAnalysis{TraceID: s.TraceID, SessionID: s.SessionID, Agent: s.Agent, Conclusion: "unknown", SkipReason: "redacted input not understandable"})
			continue
		}
		usable = append(usable, s)
	}
	if len(usable) > maxSessions {
		usable = usable[len(usable)-maxSessions:]
		out.TruncatedRange = true
	}
	out.Sessions = make([]SessionAnalysis, 0, len(usable))
	if len(usable) == 0 {
		return out, nil
	}
	payload, _ := json.Marshal(usable)
	raw, err := eino.ChatOnce(ctx, factory, cfg, evolutionprompt.AnalysisSystemPrompt, string(payload))
	if err != nil {
		return out, err
	}
	var parsed struct {
		Sessions       []SessionAnalysis `json:"sessions"`
		Gap            string            `json:"gap"`
		SuccessPattern string            `json:"success_pattern"`
	}
	if err := json.Unmarshal([]byte(stripFences(raw)), &parsed); err != nil {
		return out, fmt.Errorf("analysis output not valid JSON: %w", err)
	}
	out.Sessions = parsed.Sessions
	out.Gap = parsed.Gap
	out.SuccessPattern = parsed.SuccessPattern
	// Ensure every usable session has a conclusion; unknown when absent.
	seen := map[string]bool{}
	for _, s := range parsed.Sessions {
		seen[s.TraceID] = true
	}
	for _, s := range usable {
		if !seen[s.TraceID] {
			out.Sessions = append(out.Sessions, SessionAnalysis{TraceID: s.TraceID, SessionID: s.SessionID, Agent: s.Agent, Conclusion: "unknown"})
		}
	}
	return out, nil
}

// GenerateCandidate derives a single candidate from analysis evidence, existing
// skills and host tool scope. It cannot read the eval dataset; the caller passes
// only the sources the design allows.
func GenerateCandidate(ctx context.Context, factory eino.ModelFactory, cfg config.EinoConfig, analysis AnalysisOutput) (*Candidate, error) {
	input := map[string]any{
		"analysis":    analysis,
		"skills":      existingSkills(),
		"tool_scope":  eino.HostToolWhitelist(analysis.TargetAgent),
		"known_agents": eino.KnownAgentNames(),
	}
	payload, _ := json.Marshal(input)
	raw, err := eino.ChatOnce(ctx, factory, cfg, evolutionprompt.GenerationSystemPrompt, string(payload))
	if err != nil {
		return nil, err
	}
	candidate, err := ParseCandidate(raw)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, nil
	}
	if err := ValidateCandidate(*candidate); err != nil {
		return nil, fmt.Errorf("candidate static check failed: %w", err)
	}
	return candidate, nil
}

func existingSkills() []map[string]string {
	var out []map[string]string
	for _, name := range skills.Names() {
		document, ok := skills.ByName(name)
		if !ok {
			continue
		}
		out = append(out, map[string]string{
			"name":        document.Name,
			"description": document.Description,
			"content":     document.Content,
			"hash":        skills.Hash(document),
		})
	}
	return out
}
