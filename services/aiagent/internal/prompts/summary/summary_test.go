package prompts

import (
	"strings"
	"testing"
)

func TestSummaryPromptRequiresStrictJSONContract(t *testing.T) {
	for _, want := range []string{`"summary"`, `"key_facts"`, `"open_tasks"`, "不得新增任何字段", "禁止输出", "Markdown"} {
		if !strings.Contains(SystemPrompt, want) {
			t.Fatalf("summary prompt missing %q", want)
		}
	}
}
