package config

import (
	"log/slog"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultTaskDeadline               = 10 * time.Minute
	DefaultQueuedReconcileAfter       = 10 * time.Minute
	DefaultPollMaxInterval            = 10 * time.Second
	DefaultMaxToolIterations          = 10
	DefaultTokenBudget                = 0
	DefaultAgenticTruncatorMax    = 30
	DefaultAgenticCharacterBudget = 0 // 0 = inherit gateway.truncator.max_input_chars at daemon startup

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

	DefaultRollingTokenWindow = 5 * time.Hour
	DefaultRollingTokenLimit    = 0 // 0 = disabled
	DefaultRollingProjectedTokens = 4096
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

type QueueConfig struct {
	TaskDeadline               time.Duration
	QueuedReconcileAfter       time.Duration
	PollMaxInterval            time.Duration
	MaxToolIterations          int
	TokenBudget                int
	AgenticTruncatorMax    int
	AgenticCharacterBudget int // 0 = inherit gateway.truncator.max_input_chars (see EffectiveAgenticCharacterBudget)
	AgenticContext             AgenticContextConfig
	Instructions               InstructionsConfig
	Skills                     SkillsConfig
	HITL                       HITLConfig
	ToolTimeouts               ToolTimeoutsConfig
	ToolRetries                ToolRetriesConfig
	RollingTokenWindow         time.Duration
	RollingTokenLimit          int
}

func setQueueDefaults(v *viper.Viper) {
	v.SetDefault("queue.task_deadline", DefaultTaskDeadline.String())
	v.SetDefault("queue.queued_reconcile_after", DefaultQueuedReconcileAfter.String())
	v.SetDefault("queue.poll_max_interval", DefaultPollMaxInterval.String())
	v.SetDefault("queue.max_tool_iterations", DefaultMaxToolIterations)
	v.SetDefault("queue.token_budget", DefaultTokenBudget)
	v.SetDefault("queue.agentic_truncator_max", DefaultAgenticTruncatorMax)
	// agentic_character_budget intentionally has no SetDefault so IsSet can distinguish
	// explicit user config from the legacy agentic_truncation_threshold fallback.
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
	v.SetDefault("queue.rolling_token_window", DefaultRollingTokenWindow.String())
	v.SetDefault("queue.rolling_token_limit", DefaultRollingTokenLimit)
	setToolTimeoutDefaults(v)
	setToolRetryDefaults(v)
}

const (
	queueKeyAgenticCharacterBudget     = "queue.agentic_character_budget"
	queueKeyAgenticTruncationThreshold = "queue.agentic_truncation_threshold" // deprecated
)

// loadAgenticCharacterBudget reads queue.agentic_character_budget.
// When only the deprecated queue.agentic_truncation_threshold is set (message count),
// it logs a warning and returns 0 so the gateway truncator max_input_chars is inherited.
func loadAgenticCharacterBudget(v *viper.Viper) int {
	if v.IsSet(queueKeyAgenticCharacterBudget) {
		if v.IsSet(queueKeyAgenticTruncationThreshold) {
			slog.Warn("both config keys set; deprecated key ignored",
				"old_key", queueKeyAgenticTruncationThreshold,
				"new_key", queueKeyAgenticCharacterBudget,
			)
		}
		return v.GetInt(queueKeyAgenticCharacterBudget)
	}
	if v.IsSet(queueKeyAgenticTruncationThreshold) {
		legacy := v.GetInt(queueKeyAgenticTruncationThreshold)
		slog.Warn("deprecated config key; migrate to queue.agentic_character_budget",
			"old_key", queueKeyAgenticTruncationThreshold,
			"new_key", queueKeyAgenticCharacterBudget,
			"value", legacy,
			"note", "legacy key was message count; new key is character count; value ignored",
		)
		return DefaultAgenticCharacterBudget
	}
	return DefaultAgenticCharacterBudget
}

func loadQueueConfig(v *viper.Viper) QueueConfig {
	return QueueConfig{
		TaskDeadline:               v.GetDuration("queue.task_deadline"),
		QueuedReconcileAfter:       v.GetDuration("queue.queued_reconcile_after"),
		PollMaxInterval:            v.GetDuration("queue.poll_max_interval"),
		MaxToolIterations:          v.GetInt("queue.max_tool_iterations"),
		TokenBudget:                v.GetInt("queue.token_budget"),
		AgenticTruncatorMax:    v.GetInt("queue.agentic_truncator_max"),
		AgenticCharacterBudget: loadAgenticCharacterBudget(v),
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
		RollingTokenWindow: v.GetDuration("queue.rolling_token_window"),
		RollingTokenLimit:  v.GetInt("queue.rolling_token_limit"),
	}
}

// EffectiveAgenticCharacterBudget returns the character cap for agentic truncation.
// When agenticBudget is 0, inherits gatewayTruncatorMaxInputChars (typically gateway.truncator.max_input_chars).
func EffectiveAgenticCharacterBudget(agenticBudget, gatewayTruncatorMaxInputChars int) int {
	if agenticBudget > 0 {
		return agenticBudget
	}
	if gatewayTruncatorMaxInputChars > 0 {
		return gatewayTruncatorMaxInputChars
	}
	return 0
}
