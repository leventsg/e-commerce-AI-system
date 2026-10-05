package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LoadDataset reads and validates the fixed Skill-specific dataset. It returns the
// dataset together with a stable content hash used for version pinning.
func LoadDataset(path string) (Dataset, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Dataset{}, "", fmt.Errorf("read dataset: %w", err)
	}
	var dataset Dataset
	if err := json.Unmarshal(raw, &dataset); err != nil {
		return Dataset{}, "", fmt.Errorf("parse dataset: %w", err)
	}
	if err := ValidateDataset(dataset); err != nil {
		return Dataset{}, "", err
	}
	sum := sha256.Sum256(raw)
	return dataset, hex.EncodeToString(sum[:]), nil
}

// ValidateDataset checks structural invariants before any evaluation runs.
func ValidateDataset(dataset Dataset) error {
	if strings.TrimSpace(dataset.Version) == "" {
		return fmt.Errorf("dataset version is required")
	}
	if len(dataset.Cases) == 0 {
		return fmt.Errorf("dataset has no cases")
	}
	seen := map[string]bool{}
	for i, c := range dataset.Cases {
		if strings.TrimSpace(c.ID) == "" {
			return fmt.Errorf("case %d missing id", i)
		}
		if seen[c.ID] {
			return fmt.Errorf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		switch c.Category {
		case "applicable", "not_applicable", "boundary":
		default:
			return fmt.Errorf("case %s has invalid category %q", c.ID, c.Category)
		}
		switch c.ExpectedSkillUse {
		case "required", "forbidden", "optional":
		default:
			return fmt.Errorf("case %s has invalid expected_skill_use %q", c.ID, c.ExpectedSkillUse)
		}
		if strings.TrimSpace(c.SkillName) == "" || strings.TrimSpace(c.TargetAgent) == "" {
			return fmt.Errorf("case %s missing skill_name or target_agent", c.ID)
		}
		if strings.TrimSpace(c.Input) == "" {
			return fmt.Errorf("case %s missing input", c.ID)
		}
		if len(c.BehaviorChecks) == 0 {
			return fmt.Errorf("case %s has no behavior_checks", c.ID)
		}
		for _, check := range c.BehaviorChecks {
			if check.Type != "deterministic" && check.Type != "semantic" {
				return fmt.Errorf("case %s check %q has invalid type %q", c.ID, check.ID, check.Type)
			}
		}
		for _, fa := range c.ForbiddenActions {
			if fa.Kind != "tool" && fa.Kind != "write" {
				return fmt.Errorf("case %s has invalid forbidden action kind %q", c.ID, fa.Kind)
			}
			if fa.Kind == "tool" && strings.TrimSpace(fa.Value) == "" {
				return fmt.Errorf("case %s forbidden tool action missing value", c.ID)
			}
		}
	}
	return nil
}
