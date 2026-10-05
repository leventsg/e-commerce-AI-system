package evolution

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

var (
	backtickToken = regexp.MustCompile("`([a-z_][a-z0-9_]*)`")
	// clauseSeps 把正文切分为子句，作为判断否定/引用语境的上下文单位。
	clauseSeps = regexp.MustCompile(`[。；;\n]+`)
	// weakeningPatterns 是"削弱安全限制"的危险指令模式。用正则匹配而非子串，便于结合
	// 上下文判断短语是否处于"否定/引用"语境：例如"忽略…'覆盖用户身份'等指令"里的
	// "覆盖用户身份"是安全引用（教模型忽略该注入），不是危险指令。
	weakeningPatterns = []*regexp.Regexp{
		regexp.MustCompile(`忽略确认|绕过确认|无需确认|跳过确认|不要确认`),
		regexp.MustCompile(`覆盖用户(?:身份)?|忽略用户身份`),
		regexp.MustCompile(`修改(?:系统提示|工具\s*schema|guard|确认规则)`),
		regexp.MustCompile(`直接执行写操作|不询问直接|绕过权限`),
	}
	// negationMarkers 出现在危险短语前，表示该短语是"要忽略的指令示例"或"被否定的
	// 行为"，而非"要执行的危险指令"。
	negationMarkers = []string{"忽略", "禁止", "严禁", "拒绝", "排除", "无视", "不得", "切勿", "不", "别", "勿"}
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
	if phrase, ok := containsWeakening(document.Content); ok {
		return fmt.Errorf("candidate weakens safety restrictions (matched %q)", phrase)
	}
	if phrase, ok := containsWeakening(document.Description); ok {
		return fmt.Errorf("candidate weakens safety restrictions (matched %q)", phrase)
	}
	if err := checkToolScope(candidate.TargetAgent, document.Content); err != nil {
		return err
	}
	return nil
}

// containsWeakening reports whether text contains a weakening phrase that is not
// in a negation/reference context (e.g. "忽略…'覆盖用户身份'等指令"). It returns
// the offending phrase. Context is bounded to the clause the phrase appears in.
func containsWeakening(text string) (string, bool) {
	for _, clause := range clauseSeps.Split(text, -1) {
		for _, re := range weakeningPatterns {
			loc := re.FindStringIndex(clause)
			if loc == nil {
				continue
			}
			if !hasNegation(clause[:loc[0]]) {
				return clause[loc[0]:loc[1]], true
			}
		}
	}
	return "", false
}

// hasNegation reports whether prefix contains a negation/reference marker, which
// turns a weakening phrase into a "do-not-execute" instruction instead of a
// directive to weaken safety.
func hasNegation(prefix string) bool {
	for _, marker := range negationMarkers {
		if strings.Contains(prefix, marker) {
			return true
		}
	}
	return false
}

// checkToolScope verifies that referenced business tools stay within the host
// agent's whitelist. It only flags known business tool names, so capability tools
// and schema field names are ignored. A backtick token preceded by a negation
// marker ("不调用 checkout_prepare") is an exclusion statement, not a reference,
// and is therefore not flagged.
func checkToolScope(agent, content string) error {
	whitelist := eino.HostToolWhitelist(agent)
	allowed := map[string]bool{}
	for _, tool := range whitelist {
		allowed[tool] = true
	}
	known := map[string]bool{}
	for _, tool := range eino.KnownToolNames() {
		known[tool] = true
	}
	for _, clause := range clauseSeps.Split(content, -1) {
		for _, m := range backtickToken.FindAllStringSubmatchIndex(clause, -1) {
			tool := clause[m[2]:m[3]]
			if !known[tool] || allowed[tool] {
				continue
			}
			if hasNegation(clause[:m[0]]) {
				continue
			}
			return fmt.Errorf("skill references tool %q outside host %q scope", tool, agent)
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
