package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultTaskDeadline               = 10 * time.Minute
	DefaultQueuedReconcileAfter       = 10 * time.Minute
	DefaultPollMaxInterval            = 10 * time.Second
	DefaultMaxToolIterations          = 10
	DefaultTokenBudget                = 0
	DefaultAgenticTruncatorMax        = 30
	DefaultAgenticTruncationThreshold = 40
	DefaultAgenticCharacterBudget     = 0 // 0 = unlimited

	DefaultAnchorBudget          = 10000
	DefaultWorkingBudget         = 40000
	DefaultCompressedBudget      = 10000
	DefaultRollingThresholdTurns = 15
	DefaultKeepRecentTurns       = 5

	// DefaultInstructionsProjectFile is the convention file path relative to
	// the project workspace for project-level agent instructions.
	DefaultInstructionsProjectFile = ".agentd/AGENTS.md"

	// DefaultInstructionsUserPrefsFile is the user preferences filename
	// resolved relative to the agentd home directory (~/.agentd/).
	DefaultInstructionsUserPrefsFile = "prefs.yaml"

	// DefaultSkillsProjectDir is the relative path within a workspace for
	// project-scoped skill files.
	DefaultSkillsProjectDir = ".agentd/skills"

	// DefaultSkillsGlobalDir is the path segment under the agentd home
	// directory for user-global skills (resolved to <home>/skills at config
	// load). Absolute paths and "~/..." overrides are still supported.
	DefaultSkillsGlobalDir = "skills"

	// DefaultSkillsThreshold is the minimum TF-IDF relevance score for a
	// skill to be injected into the system prompt.
	DefaultSkillsThreshold = 0.1

	// DefaultSkillsTopK is the maximum number of skills to inject per session.
	DefaultSkillsTopK = 3

	DefaultLegacyHandoffTimeout = 7 * 24 * time.Hour

	// DefaultToolTimeout is the fallback timeout for any tool without an
	// explicit entry in ToolTimeouts.
	DefaultToolTimeout = 30 * time.Second

	// DefaultBashToolTimeout is the default timeout for bash tool calls.
	DefaultBashToolTimeout = 60 * time.Second

	// DefaultReadToolTimeout is the default timeout for read tool calls.
	DefaultReadToolTimeout = 10 * time.Second

	// DefaultWriteToolTimeout is the default timeout for write tool calls.
	DefaultWriteToolTimeout = 10 * time.Second

	// DefaultDelegateToolTimeout is the default timeout for delegate tool
	// calls. Delegation spawns sub-agents that run full agentic loops, so
	// the timeout must be generous (aligned with the task deadline).
	DefaultDelegateToolTimeout = 10 * time.Minute

	// DefaultToolRetryMaxAttempts is the default number of retry attempts
	// for tool-level transient failures.
	DefaultToolRetryMaxAttempts = 3

	// DefaultToolRetryBaseDelay is the base delay between tool retry attempts.
	DefaultToolRetryBaseDelay = 200 * time.Millisecond

	// DefaultToolRetryMaxDelay is the ceiling for exponential backoff.
	DefaultToolRetryMaxDelay = 5 * time.Second
)

// InstructionsConfig holds paths for the instruction hierarchy layers.
type InstructionsConfig struct {
	// ProjectFile is the path (relative to workspace root) for project-level
	// agent instructions. Defaults to ".agentd/AGENTS.md".
	ProjectFile string

	// UserPreferencesFile is the filename (relative to agentd home) for
	// persistent user preferences injected into every prompt.
	UserPreferencesFile string
}

type AgenticContextConfig struct {
	AnchorBudget          int
	WorkingBudget         int
	CompressedBudget      int
	RollingThresholdTurns int
	KeepRecentTurns       int
}

// SkillsConfig holds parameters for skill-based contextual knowledge injection.
type SkillsConfig struct {
	// ProjectDir is the relative path within a workspace for project-scoped
	// skill files (e.g. ".agentd/skills").
	ProjectDir string

	// GlobalDir is the resolved absolute path to the global skills directory
	// after config load (relative values are joined with agentd home; "~/..."
	// uses the user home directory; absolute paths are unchanged).
	GlobalDir string

	// Threshold is the minimum TF-IDF relevance score (0.0-1.0) for a skill
	// to be included in the system prompt.
	Threshold float64

	// TopK is the maximum number of skills injected per session.
	TopK int
}

type HITLConfig struct {
	LegacyHandoffTimeout time.Duration
}

// ToolTimeoutsConfig maps tool names to per-tool timeout durations.
// The special key "default" sets the fallback for unlisted tools.
type ToolTimeoutsConfig struct {
	Defaults map[string]time.Duration
}

// Lookup returns the timeout for the given tool name. It checks for an
// exact match first, then returns the "default" entry. If neither is
// present it returns fallback.
func (c ToolTimeoutsConfig) Lookup(toolName string, fallback time.Duration) time.Duration {
	if d, ok := c.Defaults[toolName]; ok {
		return d
	}
	if d, ok := c.Defaults["default"]; ok {
		return d
	}
	return fallback
}

// ToolRetriesConfig controls tool-level retry behaviour for transient errors.
type ToolRetriesConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Tools       map[string]struct{}
}

// Allows reports whether transparent retries are enabled for the given tool.
func (c ToolRetriesConfig) Allows(toolName string) bool {
	if len(c.Tools) == 0 {
		return false
	}
	_, ok := c.Tools[toolName]
	return ok
}

type QueueConfig struct {
	TaskDeadline               time.Duration
	QueuedReconcileAfter       time.Duration
	PollMaxInterval            time.Duration
	MaxToolIterations          int
	TokenBudget                int
	AgenticTruncatorMax        int
	AgenticTruncationThreshold int
	AgenticCharacterBudget     int
	AgenticContext             AgenticContextConfig
	Instructions               InstructionsConfig
	Skills                     SkillsConfig
	HITL                       HITLConfig
	ToolTimeouts               ToolTimeoutsConfig
	ToolRetries                ToolRetriesConfig
}

func setQueueDefaults(v *viper.Viper) {
	v.SetDefault("queue.task_deadline", DefaultTaskDeadline.String())
	v.SetDefault("queue.queued_reconcile_after", DefaultQueuedReconcileAfter.String())
	v.SetDefault("queue.poll_max_interval", DefaultPollMaxInterval.String())
	v.SetDefault("queue.max_tool_iterations", DefaultMaxToolIterations)
	v.SetDefault("queue.token_budget", DefaultTokenBudget)
	v.SetDefault("queue.agentic_truncator_max", DefaultAgenticTruncatorMax)
	v.SetDefault("queue.agentic_truncation_threshold", DefaultAgenticTruncationThreshold)
	v.SetDefault("queue.agentic_character_budget", DefaultAgenticCharacterBudget)
	v.SetDefault("queue.agentic_context.anchor_budget", DefaultAnchorBudget)
	v.SetDefault("queue.agentic_context.working_budget", DefaultWorkingBudget)
	v.SetDefault("queue.agentic_context.compressed_budget", DefaultCompressedBudget)
	v.SetDefault("queue.agentic_context.rolling_threshold_turns", DefaultRollingThresholdTurns)
	v.SetDefault("queue.agentic_context.keep_recent_turns", DefaultKeepRecentTurns)
	v.SetDefault("queue.instructions.project_file", DefaultInstructionsProjectFile)
	v.SetDefault("queue.instructions.user_preferences_file", DefaultInstructionsUserPrefsFile)
	v.SetDefault("queue.skills.project_dir", DefaultSkillsProjectDir)
	v.SetDefault("queue.skills.global_dir", DefaultSkillsGlobalDir)
	v.SetDefault("queue.skills.threshold", DefaultSkillsThreshold)
	v.SetDefault("queue.skills.top_k", DefaultSkillsTopK)
	v.SetDefault("queue.hitl.legacy_handoff_timeout", DefaultLegacyHandoffTimeout.String())
	v.SetDefault("queue.tool_timeouts.bash", DefaultBashToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.read", DefaultReadToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.write", DefaultWriteToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.delegate", DefaultDelegateToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.delegate_parallel", DefaultDelegateToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.default", DefaultToolTimeout.String())
	v.SetDefault("queue.tool_retries.max_attempts", DefaultToolRetryMaxAttempts)
	v.SetDefault("queue.tool_retries.base_delay", DefaultToolRetryBaseDelay.String())
	v.SetDefault("queue.tool_retries.max_delay", DefaultToolRetryMaxDelay.String())
	v.SetDefault("queue.tool_retries.tools", []string{"read"})
}

func loadQueueConfig(v *viper.Viper) QueueConfig {
	return QueueConfig{
		TaskDeadline:               v.GetDuration("queue.task_deadline"),
		QueuedReconcileAfter:       v.GetDuration("queue.queued_reconcile_after"),
		PollMaxInterval:            v.GetDuration("queue.poll_max_interval"),
		MaxToolIterations:          v.GetInt("queue.max_tool_iterations"),
		TokenBudget:                v.GetInt("queue.token_budget"),
		AgenticTruncatorMax:        v.GetInt("queue.agentic_truncator_max"),
		AgenticTruncationThreshold: v.GetInt("queue.agentic_truncation_threshold"),
		AgenticCharacterBudget:     v.GetInt("queue.agentic_character_budget"),
		AgenticContext: AgenticContextConfig{
			AnchorBudget:          v.GetInt("queue.agentic_context.anchor_budget"),
			WorkingBudget:         v.GetInt("queue.agentic_context.working_budget"),
			CompressedBudget:      v.GetInt("queue.agentic_context.compressed_budget"),
			RollingThresholdTurns: v.GetInt("queue.agentic_context.rolling_threshold_turns"),
			KeepRecentTurns:       v.GetInt("queue.agentic_context.keep_recent_turns"),
		},
		Instructions: InstructionsConfig{
			ProjectFile:         v.GetString("queue.instructions.project_file"),
			UserPreferencesFile: v.GetString("queue.instructions.user_preferences_file"),
		},
		Skills: SkillsConfig{
			ProjectDir: v.GetString("queue.skills.project_dir"),
			GlobalDir:  v.GetString("queue.skills.global_dir"),
			Threshold:  v.GetFloat64("queue.skills.threshold"),
			TopK:       v.GetInt("queue.skills.top_k"),
		},
		HITL: HITLConfig{
			LegacyHandoffTimeout: v.GetDuration("queue.hitl.legacy_handoff_timeout"),
		},
		ToolTimeouts: loadToolTimeoutsConfig(v),
		ToolRetries:  loadToolRetriesConfig(v),
	}
}

func loadToolRetriesConfig(v *viper.Viper) ToolRetriesConfig {
	cfg := ToolRetriesConfig{
		MaxAttempts: v.GetInt("queue.tool_retries.max_attempts"),
		BaseDelay:   v.GetDuration("queue.tool_retries.base_delay"),
		MaxDelay:    v.GetDuration("queue.tool_retries.max_delay"),
	}
	tools := v.GetStringSlice("queue.tool_retries.tools")
	if len(tools) > 0 {
		cfg.Tools = make(map[string]struct{}, len(tools))
		for _, name := range tools {
			cfg.Tools[name] = struct{}{}
		}
	}
	return cfg
}

func loadToolTimeoutsConfig(v *viper.Viper) ToolTimeoutsConfig {
	knownKeys := []string{"bash", "read", "write", "delegate", "delegate_parallel", "default"}
	result := ToolTimeoutsConfig{Defaults: make(map[string]time.Duration, len(knownKeys))}
	for _, k := range knownKeys {
		if d := v.GetDuration("queue.tool_timeouts." + k); d > 0 {
			result.Defaults[k] = d
		}
	}
	raw := v.GetStringMap("queue.tool_timeouts")
	for k := range raw {
		if _, exists := result.Defaults[k]; exists {
			continue
		}
		if d := v.GetDuration("queue.tool_timeouts." + k); d > 0 {
			result.Defaults[k] = d
		}
	}
	return result
}
