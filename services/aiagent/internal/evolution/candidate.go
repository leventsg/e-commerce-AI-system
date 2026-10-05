package evolution

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

// ParseCandidate parses the generator's JSON output into a Candidate. It returns
// (nil, nil) when the generator decided there is no candidate. Raw text wrapped
// in markdown code fences is tolerated.
func ParseCandidate(raw string) (*Candidate, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	var probe struct {
		NoCandidate bool `json:"no_candidate"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, fmt.Errorf("candidate output is not valid JSON: %w", err)
	}
	if probe.NoCandidate {
		return nil, nil
	}
	var candidate Candidate
	if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
		return nil, fmt.Errorf("candidate output is not valid JSON: %w", err)
	}
	return &candidate, nil
}

// ValidateCandidate performs the necessary static checks only. It never performs
// unlimited auto-repair: a failed check terminates the run.
func ValidateCandidate(candidate Candidate) error {
	if candidate.Operation != "add" && candidate.Operation != "modify" {
		return fmt.Errorf("invalid operation %q", candidate.Operation)
	}
	if strings.TrimSpace(candidate.SkillName) == "" || strings.TrimSpace(candidate.TargetAgent) == "" {
		return fmt.Errorf("skill_name and target_agent are required")
	}
	document, err := skills.Parse(candidate.SkillMarkdown)
	if err != nil {
		return fmt.Errorf("invalid SKILL.md: %w", err)
	}
	if document.Name != candidate.SkillName {
		return fmt.Errorf("skill_name %q does not match frontmatter name %q", candidate.SkillName, document.Name)
	}
	if !containsString(eino.KnownAgentNames(), candidate.TargetAgent) {
		return fmt.Errorf("unknown target agent %q", candidate.TargetAgent)
	}
	if candidate.Operation == "modify" {
		hash, ok := eino.EmbeddedSkillHash(candidate.SkillName)
		if !ok {
			return fmt.Errorf("cannot modify unknown skill %q", candidate.SkillName)
		}
		if candidate.BaselineSkillHash != "" && candidate.BaselineSkillHash != hash {
			return fmt.Errorf("baseline hash mismatch for %q", candidate.SkillName)
		}
	} else {
		if _, ok := skills.ByName(candidate.SkillName); ok {
			return fmt.Errorf("skill %q already exists; use modify", candidate.SkillName)
		}
		if len(candidate.Bindings) == 0 {
			return fmt.Errorf("add candidate requires bindings")
		}
		bound, ok := candidate.Bindings[candidate.TargetAgent]
		if !ok || !containsString(bound, candidate.SkillName) {
			return fmt.Errorf("add candidate bindings must include %q on %q", candidate.SkillName, candidate.TargetAgent)
		}
		for agent, names := range candidate.Bindings {
			if !containsString(eino.KnownAgentNames(), agent) {
				return fmt.Errorf("binding references unknown agent %q", agent)
			}
			for _, name := range names {
				if name == candidate.SkillName {
					continue
				}
				if _, exists := skills.ByName(name); !exists {
					return fmt.Errorf("binding references unknown skill %q", name)
				}
			}
		}
	}
	return nil
}

// SkillSet builds the B-group SkillSet for a candidate: modify replaces the target
// document; add appends the new skill via explicit bindings. The A group uses an
// empty SkillSet (embedded defaults).
func (c Candidate) SkillSet() (eino.SkillSet, error) {
	document, err := skills.Parse(c.SkillMarkdown)
	if err != nil {
		return eino.SkillSet{}, err
	}
	set := eino.SkillSet{Documents: map[string]skills.Document{document.Name: document}}
	if c.Operation == "add" {
		set.Bindings = c.Bindings
	}
	return set, nil
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
