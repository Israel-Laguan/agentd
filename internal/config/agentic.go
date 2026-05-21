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
	DefaultPlanningMaxRedoPasses   = 3
	DefaultPlanContextMaxChars     = 4000
	DefaultTopicGuardSensitivity     = 0.5
	DefaultModelRoutingContextTokens = 150000
)

// TopicGuardConfig controls topic drift detection in the agentic loop.
type TopicGuardConfig struct {
	Enabled     bool
	Sensitivity float64 // 0–1; higher = more willing to declare drift
}

// ModelTierTarget maps a complexity tier to a provider and model.
type ModelTierTarget struct {
	Provider string
	Model    string
}

// ModelRoutingConfig controls keyword-based complexity routing to model tiers.
type ModelRoutingConfig struct {
	Enabled               bool
	ContextTokenThreshold int
	Cheap                 ModelTierTarget
	Mid                   ModelTierTarget
	High                  ModelTierTarget
}

// AgenticPlanningConfig controls two-phase plan→execute and targeted section redo.
type AgenticPlanningConfig struct {
	// ComplexityThreshold is the minimum EstimateTaskComplexity score to run
	// the plan phase. 0 disables planning and targeted redo entirely.
	ComplexityThreshold int
	// MaxRedoPasses is the per-step cap for targeted section repair.
	MaxRedoPasses int
	// PlanContextMaxChars caps title+description sent to the plan model.
	PlanContextMaxChars int
}

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
	// Planning configures plan→execute splitting and targeted redo.
	Planning AgenticPlanningConfig
	// TopicGuard configures session topic drift detection.
	TopicGuard TopicGuardConfig
	// ModelRouting maps task complexity to provider/model tiers.
	ModelRouting ModelRoutingConfig
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
	v.SetDefault("agentic.planning.complexity_threshold", 0)
	v.SetDefault("agentic.planning.max_redo_passes", DefaultPlanningMaxRedoPasses)
	v.SetDefault("agentic.planning.plan_context_max_chars", DefaultPlanContextMaxChars)
	v.SetDefault("agentic.topic_guard.enabled", true)
	v.SetDefault("agentic.topic_guard.sensitivity", DefaultTopicGuardSensitivity)
	v.SetDefault("agentic.model_routing.enabled", false)
	v.SetDefault("agentic.model_routing.context_token_threshold", DefaultModelRoutingContextTokens)
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
		Planning:     loadAgenticPlanningConfig(v),
		TopicGuard:   loadTopicGuardConfig(v),
		ModelRouting: loadModelRoutingConfig(v),
	}
}

func loadModelRoutingConfig(v *viper.Viper) ModelRoutingConfig {
	threshold := v.GetInt("agentic.model_routing.context_token_threshold")
	if threshold <= 0 {
		threshold = DefaultModelRoutingContextTokens
	}
	return ModelRoutingConfig{
		Enabled:               v.GetBool("agentic.model_routing.enabled"),
		ContextTokenThreshold: threshold,
		Cheap:                 loadModelTierTarget(v, "cheap"),
		Mid:                   loadModelTierTarget(v, "mid"),
		High:                  loadModelTierTarget(v, "high"),
	}
}

func loadModelTierTarget(v *viper.Viper, tier string) ModelTierTarget {
	prefix := "agentic.model_routing." + tier
	return ModelTierTarget{
		Provider: v.GetString(prefix + ".provider"),
		Model:    v.GetString(prefix + ".model"),
	}
}

func loadTopicGuardConfig(v *viper.Viper) TopicGuardConfig {
	sensitivity := v.GetFloat64("agentic.topic_guard.sensitivity")
	if sensitivity < 0 {
		sensitivity = 0
	}
	if sensitivity > 1 {
		sensitivity = 1
	}
	if sensitivity == 0 && !v.IsSet("agentic.topic_guard.sensitivity") {
		sensitivity = DefaultTopicGuardSensitivity
	}
	return TopicGuardConfig{
		Enabled:     v.GetBool("agentic.topic_guard.enabled"),
		Sensitivity: sensitivity,
	}
}

func loadAgenticPlanningConfig(v *viper.Viper) AgenticPlanningConfig {
	complexity := v.GetInt("agentic.planning.complexity_threshold")
	if complexity < 0 {
		complexity = 0
	}
	maxRedo := v.GetInt("agentic.planning.max_redo_passes")
	if maxRedo <= 0 {
		maxRedo = DefaultPlanningMaxRedoPasses
	}
	planCtx := v.GetInt("agentic.planning.plan_context_max_chars")
	if planCtx <= 0 {
		planCtx = DefaultPlanContextMaxChars
	}
	return AgenticPlanningConfig{
		ComplexityThreshold: complexity,
		MaxRedoPasses:       maxRedo,
		PlanContextMaxChars: planCtx,
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
