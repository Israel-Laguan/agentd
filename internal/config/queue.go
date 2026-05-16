package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultTaskDeadline            = 10 * time.Minute
	DefaultQueuedReconcileAfter    = 10 * time.Minute
	DefaultPollMaxInterval         = 10 * time.Second
	DefaultMaxToolIterations       = 10
	DefaultTokenBudget             = 0
	DefaultAgenticTruncatorMax     = 30
	DefaultAgenticTruncationThreshold = 40
	DefaultAgenticCharacterBudget = 0 // 0 = unlimited

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
	}
}
