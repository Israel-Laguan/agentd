package tools

import (
	"os"
	"strings"
)

// BuildSandboxEnv assembles environment variable pairs for sandbox execution.
func BuildSandboxEnv(allowlist, extra []string) []string {
	allowed := map[string]struct{}{}
	for _, key := range allowlist {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	env := make([]string, 0, len(allowed)+len(extra))
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if _, ok := allowed[parts[0]]; ok {
			env = append(env, pair)
		}
	}
	for _, pair := range extra {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		env = append(env, pair)
	}
	return env
}
