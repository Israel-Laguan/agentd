package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

// NewSchedulerFromConfig constructs a scheduler and seeds the registry from config.
func NewSchedulerFromConfig(
	ctx context.Context,
	board models.KanbanStore,
	sink models.EventSink,
	agentic config.AgenticConfig,
	librarian config.LibrarianConfig,
) (*Scheduler, error) {
	schedStore, ok := board.(models.ScheduledTaskStore)
	if !ok {
		return nil, fmt.Errorf("kanban store does not implement ScheduledTaskStore")
	}
	providers := defaultContextProviders(board, librarian)
	s := NewScheduler(schedStore, sink, SchedulerOptions{
		Enabled:   agentic.Scheduler.Enabled,
		ProjectID: agentic.Scheduler.ProjectID,
		Providers: providers,
	})
	if err := s.BootstrapFromConfig(ctx, agentic.Scheduler); err != nil {
		return nil, err
	}
	return s, nil
}

// BootstrapFromConfig upserts configured tasks into the persisted registry.
func (s *Scheduler) BootstrapFromConfig(ctx context.Context, cfg config.SchedulerConfig) error {
	if s.store == nil {
		return nil
	}
	for _, t := range cfg.Tasks {
		if strings.TrimSpace(t.ID) == "" {
			continue
		}
		entry, err := configTaskToModel(t)
		if err != nil {
			return fmt.Errorf("scheduled task %q: %w", t.ID, err)
		}
		if strings.TrimSpace(entry.CronExpr) != "" {
			sched, err := config.ParseCronExpr(entry.CronExpr)
			if err != nil {
				return fmt.Errorf("scheduled task %q cron: %w", t.ID, err)
			}
			s.cronByID[entry.ID] = cachedCron{expr: entry.CronExpr, sched: sched}
		}
		if err := s.store.UpsertScheduledTask(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func configTaskToModel(t config.SchedulerTaskConfig) (models.ScheduledTask, error) {
	entry := models.ScheduledTask{
		ID:                  t.ID,
		CronExpr:            t.CronExpr,
		TaskType:            t.TaskType,
		ContextFn:           t.ContextFn,
		ContextArgs:         t.ContextArgs,
		OutputTarget:        t.OutputTarget,
		Title:               t.Title,
		DescriptionTemplate: t.DescriptionTemplate,
		ProjectID:           t.ProjectID,
		Kind:                models.ScheduledTaskKindDispatch,
		Enabled:             true,
	}
	if entry.OutputTarget == "" {
		entry.OutputTarget = config.DefaultSchedulerOutputTarget
	}
	if strings.TrimSpace(t.RunAfter) != "" {
		parsed, err := time.Parse(time.RFC3339, t.RunAfter)
		if err != nil {
			if parsed, err = time.Parse(time.RFC3339Nano, t.RunAfter); err != nil {
				return entry, fmt.Errorf("invalid run_after %q: %w", t.RunAfter, err)
			}
		}
		entry.RunAfter = &parsed
	}
	return entry, nil
}
