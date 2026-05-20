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
	DefaultFileContextTopK         = 5
	DefaultFileContextCachePath    = "file-cache"
	DefaultFileContextEmbedModel   = "text-embedding-3-small"
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
	// FileContext configures smart file preprocessing for the read tool.
	FileContext FileContextConfig
}

// FileContextConfig controls convert/cache/select pipeline for workspace files.
type FileContextConfig struct {
	Enabled        bool
	TopK           int
	CachePath      string
	EmbeddingModel string
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
	v.SetDefault("agentic.file_context.enabled", false)
	v.SetDefault("agentic.file_context.top_k", DefaultFileContextTopK)
	v.SetDefault("agentic.file_context.cache_path", DefaultFileContextCachePath)
	v.SetDefault("agentic.file_context.embedding_model", DefaultFileContextEmbedModel)
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
		FileContext: loadFileContextConfig(v),
	}
}

func loadFileContextConfig(v *viper.Viper) FileContextConfig {
	topK := v.GetInt("agentic.file_context.top_k")
	if topK <= 0 {
		topK = DefaultFileContextTopK
	}
	cachePath := v.GetString("agentic.file_context.cache_path")
	if cachePath == "" {
		cachePath = DefaultFileContextCachePath
	}
	model := v.GetString("agentic.file_context.embedding_model")
	if model == "" {
		model = DefaultFileContextEmbedModel
	}
	return FileContextConfig{
		Enabled:        v.GetBool("agentic.file_context.enabled"),
		TopK:           topK,
		CachePath:      cachePath,
		EmbeddingModel: model,
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

// ResolveFileContextCachePath returns the absolute file-context cache directory.
func ResolveFileContextCachePath(homeDir, raw string) string {
	if raw == "" {
		raw = DefaultFileContextCachePath
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	return filepath.Join(homeDir, raw)
}
