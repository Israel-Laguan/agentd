package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultTaskDeadline            = 10 * time.Minute
	DefaultPollMaxInterval         = 10 * time.Second
	DefaultMaxToolIterations       = 10
	DefaultTokenBudget             = 0
	DefaultAgenticTruncatorMax     = 30
	DefaultAgenticTruncationThreshold = 40
	DefaultAgenticCharacterBudget = 0 // 0 = unlimited

	// DefaultInstructionsProjectFile is the convention file path relative to
	// the project workspace for project-level agent instructions.
	DefaultInstructionsProjectFile = ".agentd/AGENTS.md"

	// DefaultInstructionsUserPrefsFile is the user preferences filename
	// resolved relative to the agentd home directory (~/.agentd/).
	DefaultInstructionsUserPrefsFile = "prefs.yaml"
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

type QueueConfig struct {
	TaskDeadline               time.Duration
	PollMaxInterval            time.Duration
	MaxToolIterations          int
	TokenBudget                int
	AgenticTruncatorMax        int
	AgenticTruncationThreshold int
	AgenticCharacterBudget     int
	Instructions               InstructionsConfig
}

func setQueueDefaults(v *viper.Viper) {
	v.SetDefault("queue.task_deadline", DefaultTaskDeadline.String())
	v.SetDefault("queue.poll_max_interval", DefaultPollMaxInterval.String())
	v.SetDefault("queue.max_tool_iterations", DefaultMaxToolIterations)
	v.SetDefault("queue.token_budget", DefaultTokenBudget)
	v.SetDefault("queue.agentic_truncator_max", DefaultAgenticTruncatorMax)
	v.SetDefault("queue.agentic_truncation_threshold", DefaultAgenticTruncationThreshold)
	v.SetDefault("queue.agentic_character_budget", DefaultAgenticCharacterBudget)
	v.SetDefault("queue.instructions.project_file", DefaultInstructionsProjectFile)
	v.SetDefault("queue.instructions.user_preferences_file", DefaultInstructionsUserPrefsFile)
}

func loadQueueConfig(v *viper.Viper) QueueConfig {
	return QueueConfig{
		TaskDeadline:               v.GetDuration("queue.task_deadline"),
		PollMaxInterval:            v.GetDuration("queue.poll_max_interval"),
		MaxToolIterations:          v.GetInt("queue.max_tool_iterations"),
		TokenBudget:                v.GetInt("queue.token_budget"),
		AgenticTruncatorMax:        v.GetInt("queue.agentic_truncator_max"),
		AgenticTruncationThreshold: v.GetInt("queue.agentic_truncation_threshold"),
		AgenticCharacterBudget:     v.GetInt("queue.agentic_character_budget"),
		Instructions: InstructionsConfig{
			ProjectFile:         v.GetString("queue.instructions.project_file"),
			UserPreferencesFile: v.GetString("queue.instructions.user_preferences_file"),
		},
	}
}
