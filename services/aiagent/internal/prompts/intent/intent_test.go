package prompts

import (
	"strings"
	"testing"
)

func TestIntentPlannerSystemContainsRequiredConstraints(t *testing.T) {
	prompt := IntentSystemPrompt
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("IntentPlannerSystem should not be empty")
	}

	for _, want := range []string{
		"只返回 JSON",
		"intent",
		"tool_name",
		"arguments 不得包含 user_id",
		"missing_params",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("IntentPlannerSystem missing %q: %s", want, prompt)
		}
	}
}
