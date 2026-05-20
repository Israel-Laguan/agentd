package config

import (
	"path/filepath"

	"github.com/spf13/viper"
)

// AgenticConfig holds agent-loop settings documented under the top-level
// agentic: key in config.reference.yaml.
const (
	DefaultContextWarningThreshold = 0.85
	DefaultToolFailureStreak       = 3
)

type AgenticConfig struct {
	// ContextWarningThreshold is the fraction (0–1) of the zone character budget
	// at which preemptive summarization runs. 0 disables the warning path.
	ContextWarningThreshold float64
	// ToolFailureStreak is consecutive tool errors on the same tool before the
	// loop stops with LoopToolFailure. 0 means fatal-only aborts.
	ToolFailureStreak int
	// ExternalTools lists tool names whose results are wrapped in
	// <external_content> tags. When empty, all non-builtin tools are wrapped.
	ExternalTools []string
	// ToolCredentials maps tool names to environment variable names holding credentials.
	ToolCredentials map[string]string
	// DisableCredentialDetection skips CredentialDetectionHook when true.
	// Disabling reduces defense-in-depth; secrets in tool args may reach logs/context.
	DisableCredentialDetection bool
	// Audit configures structured JSONL audit logging for tool dispatches.
	Audit AuditConfig
}

// AuditConfig controls the structured audit log file separate from the SSE event stream.
type AuditConfig struct {
	Enabled bool
	Path    string
}

func setAgenticDefaults(v *viper.Viper) {
	v.SetDefault("agentic.context_warning_threshold", DefaultContextWarningThreshold)
	v.SetDefault("agentic.tool_failure_streak", DefaultToolFailureStreak)
	v.SetDefault("agentic.external_tools", []string{})
	v.SetDefault("agentic.audit.enabled", false)
	v.SetDefault("agentic.audit.path", "audit.jsonl")
}

func loadAgenticConfig(v *viper.Viper) AgenticConfig {
	threshold := v.GetFloat64("agentic.context_warning_threshold")
	if threshold < 0 || threshold > 1 {
		threshold = DefaultContextWarningThreshold
	}
	return AgenticConfig{
		ContextWarningThreshold:    threshold,
		ToolFailureStreak:          v.GetInt("agentic.tool_failure_streak"),
		ExternalTools:              v.GetStringSlice("agentic.external_tools"),
		ToolCredentials:            v.GetStringMapString("agentic.tool_credentials"),
		DisableCredentialDetection: v.GetBool("agentic.disable_credential_detection"),
		Audit: AuditConfig{
			Enabled: v.GetBool("agentic.audit.enabled"),
			Path:    v.GetString("agentic.audit.path"),
		},
	}
}

// ResolveAuditPath returns the absolute audit log path. Relative paths are joined with homeDir.
func ResolveAuditPath(homeDir, raw string) string {
	if raw == "" {
		raw = "audit.jsonl"
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	return filepath.Join(homeDir, raw)
}
