package eino

import (
	"sort"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

// KnownAgentNames returns every agent that can host a skill, in stable order.
// It is a read-only view used for candidate validation.
func KnownAgentNames() []string {
	names := []string{supervisorAgentName}
	for _, spec := range supervisorSubAgentSpecs {
		names = append(names, spec.name)
	}
	sort.Strings(names)
	return names
}

// SkillBindings returns a copy of the process-level agent→skill bindings. It never
// returns the internal map, so callers cannot mutate the global binding table.
func SkillBindings() map[string][]string {
	out := make(map[string][]string, len(agentSkillBindings))
	for agent, names := range agentSkillBindings {
		out[agent] = append([]string(nil), names...)
	}
	return out
}

// HostToolWhitelist returns the business tool names an agent may reference. The
// supervisor may delegate to any sub-agent, so its whitelist is the union of all
// sub-agent tools plus the sub-agent names themselves.
func HostToolWhitelist(agent string) []string {
	if agent == supervisorAgentName {
		seen := map[string]bool{}
		var out []string
		for _, spec := range supervisorSubAgentSpecs {
			seen[spec.name] = true
			out = append(out, spec.name)
			for _, tool := range spec.tools {
				if !seen[tool] {
					seen[tool] = true
					out = append(out, tool)
				}
			}
		}
		sort.Strings(out)
		return out
	}
	for _, spec := range supervisorSubAgentSpecs {
		if spec.name == agent {
			return append([]string(nil), spec.tools...)
		}
	}
	return nil
}

// EmbeddedSkillHash returns the content hash of the embedded skill with the given
// name. The second return is false when the skill does not exist.
func EmbeddedSkillHash(name string) (string, bool) {
	document, ok := skills.ByName(name)
	if !ok {
		return "", false
	}
	return skills.Hash(document), true
}

// KnownToolNames returns the full business tool catalog names in stable order.
// Capability tools (get_tool_call_result, search_user_memory) are not part of the
// business catalog and are therefore not matched by candidate tool-scope checks.
func KnownToolNames() []string {
	catalog := tools.DefaultBusinessTools(tools.DefaultToolClients{}, config.ToolTimeoutConfig{})
	names := make([]string, 0, len(catalog))
	for _, tool := range catalog {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}
