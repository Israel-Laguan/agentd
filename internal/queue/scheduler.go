package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"agentd/internal/config"
	"agentd/internal/models"
	qw "agentd/internal/queue/worker"
)

type cachedCron struct {
	expr  string
	sched cron.Schedule
}

// ContextProvider resolves live context for a scheduled dispatch at runtime.
type ContextProvider interface {
	Fetch(ctx context.Context, entry models.ScheduledTask, projectID string) (string, error)
}

// ContextProviderRegistry maps context_fn names to providers.
type ContextProviderRegistry map[string]ContextProvider

// Scheduler dispatches registry entries on a minute-resolution tick.
type Scheduler struct {
	store       models.ScheduledTaskStore
	sink        models.EventSink
	enabled     bool
	projectID   string
	providers   ContextProviderRegistry
	cronByID    map[string]cachedCron
}

// SchedulerOptions configures a Scheduler instance.
type SchedulerOptions struct {
	Enabled   bool
	ProjectID string
	Providers ContextProviderRegistry
}

// NewScheduler builds a scheduler. When disabled, Tick is a no-op.
func NewScheduler(store models.ScheduledTaskStore, sink models.EventSink, opts SchedulerOptions) *Scheduler {
	if opts.Providers == nil {
		opts.Providers = ContextProviderRegistry{}
	}
	return &Scheduler{
		store:     store,
		sink:      sink,
		enabled:   opts.Enabled,
		projectID: opts.ProjectID,
		providers: opts.Providers,
		cronByID:  make(map[string]cachedCron),
	}
}

// Enabled reports whether the scheduler runs ticks and defers.
func (s *Scheduler) Enabled() bool {
	return s != nil && s.enabled
}

// ScheduleDeferred registers a one-shot requeue for a deferred kanban task.
func (s *Scheduler) ScheduleDeferred(ctx context.Context, taskID string, runAfter time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.ScheduleDeferredRequeue(ctx, taskID, runAfter)
}

// Tick evaluates due registry entries at the given time (truncated to UTC minute).
func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {
	if s == nil || !s.enabled || s.store == nil {
		return nil
	}
	entries, err := s.store.ListScheduledTasks(ctx)
	if err != nil {
		return err
	}
	slot := now.UTC().Truncate(time.Minute)
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		if err := s.tickEntry(ctx, entry, slot); err != nil {
			slog.Error("scheduler entry failed", "id", entry.ID, "error", err)
		}
	}
	return nil
}

func (s *Scheduler) tickEntry(ctx context.Context, entry models.ScheduledTask, slot time.Time) error {
	if entry.Kind == models.ScheduledTaskKindRequeue {
		if !runAfterDue(entry, slot) {
			return nil
		}
		return s.fireRequeue(ctx, entry)
	}
	if !s.cronDue(entry, slot) && !runAfterDue(entry, slot) {
		return nil
	}
	return s.fireDispatch(ctx, entry, slot)
}

func (s *Scheduler) cronDue(entry models.ScheduledTask, slot time.Time) bool {
	if strings.TrimSpace(entry.CronExpr) == "" {
		return false
	}
	cc, ok := s.cronByID[entry.ID]
	if !ok || cc.expr != entry.CronExpr || cc.sched == nil {
		sched, err := config.ParseCronExpr(entry.CronExpr)
		if err != nil {
			slog.Error("scheduler invalid cron", "id", entry.ID, "error", err)
			return false
		}
		cc = cachedCron{expr: entry.CronExpr, sched: sched}
		s.cronByID[entry.ID] = cc
	}
	prev := slot.Add(-time.Minute)
	if entry.LastFiredAt != nil && !entry.LastFiredAt.Before(slot) {
		return false
	}
	return cc.sched.Next(prev).Equal(slot)
}

func runAfterDue(entry models.ScheduledTask, slot time.Time) bool {
	if entry.RunAfter == nil || entry.RunAfter.IsZero() {
		return false
	}
	if entry.LastFiredAt != nil && !entry.LastFiredAt.IsZero() {
		return false
	}
	return !entry.RunAfter.After(slot)
}

func (s *Scheduler) fireRequeue(ctx context.Context, entry models.ScheduledTask) error {
	if entry.TargetTaskID == "" {
		return s.store.DeleteScheduledTask(ctx, entry.ID)
	}
	current, err := s.store.GetTask(ctx, entry.TargetTaskID)
	if err != nil {
		if errors.Is(err, models.ErrTaskNotFound) {
			return s.store.DeleteScheduledTask(ctx, entry.ID)
		}
		return err
	}
	if current.State == models.TaskStateQueued {
		if _, err := s.store.UpdateTaskState(ctx, entry.TargetTaskID, current.UpdatedAt, models.TaskStateReady); err != nil {
			return err
		}
	}
	if err := s.store.UpdateScheduledTaskLastFired(ctx, entry.ID, time.Now().UTC()); err != nil {
		return err
	}
	return s.store.DeleteScheduledTask(ctx, entry.ID)
}

func (s *Scheduler) fireDispatch(ctx context.Context, entry models.ScheduledTask, slot time.Time) error {
	projectID, err := s.resolveProjectID(ctx, entry)
	if err != nil {
		return err
	}
	contextBody, err := s.resolveContext(ctx, entry, projectID)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(entry.Title)
	if title == "" {
		title = entry.ID
	}
	title = fmt.Sprintf("%s [%s]", title, slot.Format("2006-01-02 15:04"))
	description := buildScheduledDescription(entry, contextBody)
	oneShot := entry.RunAfter != nil && strings.TrimSpace(entry.CronExpr) == ""
	task, err := s.store.InsertReadyTaskAndRecordDispatch(ctx, projectID, models.DraftTask{
		Title:       title,
		Description: description,
		Assignee:    models.TaskAssigneeSystem,
	}, entry.ID, slot, oneShot)
	if err != nil {
		return err
	}
	return s.emitDispatchEvent(ctx, entry, projectID, task)
}

func (s *Scheduler) resolveProjectID(ctx context.Context, entry models.ScheduledTask) (string, error) {
	if strings.TrimSpace(entry.ProjectID) != "" {
		return entry.ProjectID, nil
	}
	if strings.TrimSpace(s.projectID) != "" {
		return s.projectID, nil
	}
	project, err := s.store.EnsureSystemProject(ctx)
	if err != nil {
		return "", err
	}
	return project.ID, nil
}

func (s *Scheduler) resolveContext(ctx context.Context, entry models.ScheduledTask, projectID string) (string, error) {
	name := strings.TrimSpace(entry.ContextFn)
	if name == "" || name == "static" {
		return staticContextProvider{}.Fetch(ctx, entry, projectID)
	}
	provider, ok := s.providers[name]
	if !ok {
		return "", fmt.Errorf("unknown context_fn %q", name)
	}
	return provider.Fetch(ctx, entry, projectID)
}

func buildScheduledDescription(entry models.ScheduledTask, contextBody string) string {
	var b strings.Builder
	if tmpl := strings.TrimSpace(entry.DescriptionTemplate); tmpl != "" {
		b.WriteString(tmpl)
		b.WriteString("\n\n")
	}
	if contextBody != "" {
		b.WriteString(contextBody)
		b.WriteString("\n\n")
	}
	if tt := strings.TrimSpace(entry.TaskType); tt != "" {
		b.WriteString("Scheduled task type: ")
		b.WriteString(tt)
		b.WriteString("\n")
		b.WriteString(taskTypeKeywords(tt))
	}
	return strings.TrimSpace(b.String())
}

func taskTypeKeywords(taskType string) string {
	switch taskType {
	case qw.TaskTypeSummarize:
		return "summarize summary recap"
	case qw.TaskTypeCodeGen:
		return "implement fix refactor code"
	case qw.TaskTypeDocQA:
		return "explain document readme"
	case qw.TaskTypeWebResearch:
		return "search fetch web url"
	case qw.TaskTypeFullAgent:
		return "orchestrate multi-step deploy"
	default:
		return taskType
	}
}

func (s *Scheduler) emitDispatchEvent(ctx context.Context, entry models.ScheduledTask, projectID string, task *models.Task) error {
	if s.sink == nil {
		return nil
	}
	evType := normalizeOutputTarget(entry.OutputTarget)
	return s.sink.Emit(ctx, models.Event{
		ProjectID: projectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      evType,
		Payload:   fmt.Sprintf("scheduled_id=%s task_type=%s", entry.ID, entry.TaskType),
	})
}

func normalizeOutputTarget(raw string) models.EventType {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "RESULT":
		return models.EventTypeResult
	case "LOG", "":
		return models.EventTypeLog
	default:
		return models.EventType(raw)
	}
}
