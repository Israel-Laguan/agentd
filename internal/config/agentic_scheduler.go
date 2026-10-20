package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"agentd/internal/models"
)

const (
	DefaultSchedulerTickInterval = time.Minute
	DefaultSchedulerOutputTarget = "LOG"
)

// SchedulerTaskConfig is one recurring or static scheduled job from config.
type SchedulerTaskConfig struct {
	ID                  string            `mapstructure:"id"`
	CronExpr            string            `mapstructure:"cron_expr"`
	RunAfter            string            `mapstructure:"run_after"`
	TaskType            string            `mapstructure:"task_type"`
	ContextFn           string            `mapstructure:"context_fn"`
	ContextArgs         map[string]string `mapstructure:"context_args"`
	OutputTarget        string            `mapstructure:"output_target"`
	Title               string            `mapstructure:"title"`
	DescriptionTemplate string            `mapstructure:"description_template"`
	ProjectID           string            `mapstructure:"project_id"`
}

// SchedulerConfig controls the agentic task scheduler registry and tick.
type SchedulerConfig struct {
	Enabled      bool
	TickInterval time.Duration
	ProjectID    string
	Tasks        []SchedulerTaskConfig
}

func setSchedulerDefaults(v *viper.Viper) {
	v.SetDefault("agentic.scheduler.enabled", false)
	v.SetDefault("agentic.scheduler.tick_interval", "1m")
	v.SetDefault("agentic.scheduler.project_id", "")
}

func loadSchedulerConfig(v *viper.Viper) (SchedulerConfig, error) {
	tick := v.GetDuration("agentic.scheduler.tick_interval")
	if tick <= 0 {
		tick = DefaultSchedulerTickInterval
	}
	tasks, err := loadSchedulerTasks(v)
	if err != nil {
		return SchedulerConfig{}, fmt.Errorf("decode agentic.scheduler.tasks: %w", err)
	}
	cfg := SchedulerConfig{
		Enabled:      v.GetBool("agentic.scheduler.enabled"),
		TickInterval: tick,
		ProjectID:    v.GetString("agentic.scheduler.project_id"),
		Tasks:        tasks,
	}
	if err := cfg.validateProjects(); err != nil {
		return SchedulerConfig{}, err
	}
	return cfg, nil
}

// validateProjects rejects an enabled scheduler whose task resolves to no
// project, or to _system: either way it lands in _system, which the worker
// refuses to execute (B-076).
func (c SchedulerConfig) validateProjects() error {
	if !c.Enabled {
		return nil
	}
	if isSystemProject(c.ProjectID) {
		return fmt.Errorf("agentic.scheduler.project_id %q is the _system project, which cannot run tasks", c.ProjectID)
	}
	for _, t := range c.Tasks {
		if strings.TrimSpace(t.ID) == "" {
			continue
		}
		if isSystemProject(t.ProjectID) {
			return fmt.Errorf("agentic.scheduler: task %q project_id %q is the _system project, which cannot run tasks", t.ID, t.ProjectID)
		}
		if strings.TrimSpace(t.ProjectID) == "" && strings.TrimSpace(c.ProjectID) == "" {
			return fmt.Errorf("agentic.scheduler: task %q has no project_id and agentic.scheduler.project_id is unset; set one so the task can run", t.ID)
		}
	}
	return nil
}

// isSystemProject reports a project_id that names the _system project, by ID or name.
func isSystemProject(id string) bool {
	switch strings.TrimSpace(id) {
	case models.SystemProjectID, models.SystemProjectName:
		return true
	}
	return false
}

func loadSchedulerTasks(v *viper.Viper) ([]SchedulerTaskConfig, error) {
	if !v.IsSet("agentic.scheduler.tasks") {
		return nil, nil
	}
	var tasks []SchedulerTaskConfig
	if err := v.UnmarshalKey("agentic.scheduler.tasks", &tasks); err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].OutputTarget == "" {
			tasks[i].OutputTarget = DefaultSchedulerOutputTarget
		}
	}
	return tasks, nil
}
