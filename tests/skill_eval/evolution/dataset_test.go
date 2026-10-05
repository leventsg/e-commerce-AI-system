//go:build ai_skill_evolution

package evolution

import (
	"path/filepath"
	"testing"
)

func TestLoadDatasetValidatesSkillEvalSet(t *testing.T) {
	dataset, hash, err := LoadDataset(filepath.Join(repoRoot(t), "tests", "skill_eval", "dataset.json"))
	if err != nil {
		t.Fatalf("LoadDataset() error = %v", err)
	}
	if hash == "" {
		t.Fatal("dataset hash is empty")
	}
	counts := map[string]map[string]int{}
	for _, c := range dataset.Cases {
		if counts[c.Capability] == nil {
			counts[c.Capability] = map[string]int{}
		}
		counts[c.Capability][c.Category]++
	}
	for _, capability := range []string{"issue-triage", "purchase-advisor"} {
		for _, category := range []string{"applicable", "not_applicable", "boundary"} {
			if counts[capability][category] < 4 {
				t.Fatalf("capability %s category %s has %d cases, want >= 4", capability, category, counts[capability][category])
			}
		}
	}
}

func TestValidateDatasetRejectsInvalidCategory(t *testing.T) {
	dataset := Dataset{Version: "1", Cases: []Case{{
		ID:               "c1",
		Capability:       "x",
		TargetAgent:      "supervisor_agent",
		SkillName:        "issue-triage",
		Category:         "bogus",
		ExpectedSkillUse: "required",
		Input:            "hello",
		BehaviorChecks:   []BehaviorCheck{{ID: "b", Type: "deterministic", Check: map[string]any{"kind": "no_write_tool"}}},
	}}}
	if err := ValidateDataset(dataset); err == nil {
		t.Fatal("expected invalid category error")
	}
}

func TestValidateDatasetRequiresBehaviorChecks(t *testing.T) {
	dataset := Dataset{Version: "1", Cases: []Case{{
		ID:               "c1",
		Capability:       "x",
		TargetAgent:      "supervisor_agent",
		SkillName:        "issue-triage",
		Category:         "applicable",
		ExpectedSkillUse: "required",
		Input:            "hello",
	}}}
	if err := ValidateDataset(dataset); err == nil {
		t.Fatal("expected missing behavior_checks error")
	}
}
