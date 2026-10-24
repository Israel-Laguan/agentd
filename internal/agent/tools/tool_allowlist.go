package tools

import (
	"strings"

	"agentd/internal/gateway"
)

// ProfileAllowlist turns a profile's AllowedTools into the set that the offered
// list and the dispatch veto check against. It returns nil when no allowlist is
// set, and nil means every tool is allowed.
func ProfileAllowlist(tools []string) map[string]bool {
	allowed := normalizeToolNameSet(tools)
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

// AllowsTool reports whether a call to name may run under an allowlist from
// ProfileAllowlist. A nil allowlist allows every tool.
func AllowsTool(allowed map[string]bool, name string) bool {
	if allowed == nil {
		return true
	}
	return allowed[strings.ToLower(strings.TrimSpace(name))]
}

// FilterAllowedTools keeps only the offered tools that an allowlist lists. A nil
// allowlist returns the inputs unchanged.
func FilterAllowedTools(
	tools []gateway.ToolDefinition,
	index map[string]string,
	allowed map[string]bool,
) ([]gateway.ToolDefinition, map[string]string) {
	if allowed == nil {
		return tools, index
	}
	return filterToolsByNames(tools, index, allowed)
}
