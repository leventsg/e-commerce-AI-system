package domain

import (
	"strings"
	"testing"
)

func TestHybridEstimatorTokens(t *testing.T) {
	est := HybridEstimator{}
	cases := []struct {
		name  string
		text  string
		min   int
		max   int
		exact int
	}{
		{name: "empty", text: "", exact: 0},
		{name: "cjk", text: "你好", exact: 2},
		{name: "ascii", text: "hello", exact: 2},
		{name: "mixed", text: "你好hello", exact: 4},
		{name: "single_ascii", text: "a", exact: 1},
	}
	for _, c := range cases {
		got := est.Tokens(c.text)
		if got != c.exact {
			t.Fatalf("%s: Tokens(%q) = %d, want %d", c.name, c.text, got, c.exact)
		}
	}
	// 中文应偏高估：1 汉字 >= 1 token，保证预算不超窗。
	longCN := strings.Repeat("好", 100)
	if est.Tokens(longCN) < 100 {
		t.Fatalf("cjk underestimate: %d", est.Tokens(longCN))
	}
	// ASCII 4 字符约 1 token。
	longASCII := strings.Repeat("a", 400)
	if got := est.Tokens(longASCII); got < 95 || got > 110 {
		t.Fatalf("ascii estimate out of range: %d", got)
	}
}

func TestTruncateToTokens(t *testing.T) {
	est := HybridEstimator{}
	if got := TruncateToTokens("short", 100, est); got != "short" {
		t.Fatalf("no truncation expected, got %q", got)
	}
	if got := TruncateToTokens("你好世界", 0, est); got != "你好世界" {
		t.Fatalf("maxTokens<=0 should not truncate, got %q", got)
	}
	if got := TruncateToTokens("你好", 100, nil); got != "你好" {
		t.Fatalf("nil estimator should not truncate, got %q", got)
	}
	truncated := TruncateToTokens("你好世界", 2, est)
	if !strings.Contains(truncated, "你好") || strings.Contains(truncated, "世界") || !strings.Contains(truncated, "[truncated]") {
		t.Fatalf("truncated = %q", truncated)
	}
}

func TestTokenBudgetNormalized(t *testing.T) {
	b := TokenBudget{}.Normalized()
	if b.TotalTokens != 8000 || b.RecentWindowTokens != 3000 || b.SummaryTriggerTokens != 4000 {
		t.Fatalf("defaults = %+v", b)
	}
	if b.MinRecentMessages != 2 || b.SafetyFactor != 0.9 {
		t.Fatalf("defaults = %+v", b)
	}
	kept := TokenBudget{TotalTokens: 100, SafetyFactor: 0.5}.Normalized()
	if kept.TotalTokens != 100 || kept.SafetyFactor != 0.5 {
		t.Fatalf("kept = %+v", kept)
	}
	clamped := TokenBudget{SafetyFactor: 1.5}.Normalized()
	if clamped.SafetyFactor != 0.9 {
		t.Fatalf("safety factor should clamp to 0.9, got %v", clamped.SafetyFactor)
	}
}

func TestTokenBudgetEffectiveTotalTokens(t *testing.T) {
	b := TokenBudget{TotalTokens: 100, SafetyFactor: 0.9}
	if got := b.EffectiveTotalTokens(); got != 90 {
		t.Fatalf("EffectiveTotalTokens() = %d, want 90", got)
	}
}
