package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestSchedulerCronEveryFiveMinutes(t *testing.T) {
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	entry := models.ScheduledTask{
		ID:       "health",
		CronExpr: "*/5 * * * *",
		Title:    "Health",
		ContextFn: "static",
		ContextArgs: map[string]string{"body": "check"},
		Kind:     models.ScheduledTaskKindDispatch,
		Enabled:  true,
		ProjectID: project.ID,
	}
	if err := store.UpsertScheduledTask(context.Background(), entry); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	sched, err := config.ParseCronExpr(entry.CronExpr)
	if err != nil {
		t.Fatalf("ParseCronExpr: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	s.cronByID[entry.ID] = cachedCron{expr: entry.CronExpr, sched: sched}

	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	if err := s.Tick(context.Background(), base); err != nil {
		t.Fatalf("Tick at :00: %v", err)
	}
	if len(store.Tasks()) != 1 {
		t.Fatalf("tasks after first tick = %d, want 1", len(store.Tasks()))
	}
	if err := s.Tick(context.Background(), base.Add(2*time.Minute)); err != nil {
		t.Fatalf("Tick at :02: %v", err)
	}
	if len(store.Tasks()) != 1 {
		t.Fatalf("tasks after :02 tick = %d, want 1 (no double dispatch)", len(store.Tasks()))
	}
	if err := s.Tick(context.Background(), base.Add(5*time.Minute)); err != nil {
		t.Fatalf("Tick at :05: %v", err)
	}
	if len(store.Tasks()) != 2 {
		t.Fatalf("tasks after :05 tick = %d, want 2", len(store.Tasks()))
	}
}

func TestSchedulerRunAfterDeferredRequeue(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	_, err = store.InsertReadyTask(ctx, project.ID, models.DraftTask{
		Title: "Deferred", Assignee: models.TaskAssigneeSystem,
	})
	if err != nil {
		t.Fatalf("InsertReadyTask: %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimNextReadyTasks: %v len=%d", err, len(claimed))
	}
	task := claimed[0]

	runAfter := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := store.ScheduleDeferredRequeue(ctx, task.ID, runAfter); err != nil {
		t.Fatalf("ScheduleDeferredRequeue: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})

	before := runAfter.Add(-time.Minute)
	if err := s.Tick(ctx, before); err != nil {
		t.Fatalf("Tick before: %v", err)
	}
	got, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask before run_after: %v", err)
	}
	if got.State != models.TaskStateQueued {
		t.Fatalf("state before run_after = %s, want QUEUED", got.State)
	}

	if err := s.Tick(ctx, runAfter); err != nil {
		t.Fatalf("Tick at run_after: %v", err)
	}
	got, err = store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after run_after: %v", err)
	}
	if got.State != models.TaskStateReady {
		t.Fatalf("state after run_after = %s, want READY", got.State)
	}
	if len(store.ScheduledTasks()) != 0 {
		t.Fatalf("scheduled entries after requeue = %d, want 0", len(store.ScheduledTasks()))
	}
}

func TestSchedulerTickIdempotentSameMinute(t *testing.T) {
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	entry := models.ScheduledTask{
		ID:        "once",
		CronExpr:  "*/5 * * * *",
		Title:     "Once",
		ContextFn: "static",
		Kind:      models.ScheduledTaskKindDispatch,
		Enabled:   true,
		ProjectID: project.ID,
	}
	if err := store.UpsertScheduledTask(context.Background(), entry); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	sched, err := config.ParseCronExpr(entry.CronExpr)
	if err != nil {
		t.Fatalf("ParseCronExpr: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	s.cronByID[entry.ID] = cachedCron{expr: entry.CronExpr, sched: sched}

	slot := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	if err := s.Tick(context.Background(), slot); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if err := s.Tick(context.Background(), slot.Add(30*time.Second)); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if len(store.Tasks()) != 1 {
		t.Fatalf("tasks = %d, want 1 (idempotent same minute)", len(store.Tasks()))
	}
}

type processRecordingWorker struct {
	mu    sync.Mutex
	calls []string
}

func (w *processRecordingWorker) Process(_ context.Context, task models.Task) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, task.ID)
}

func (w *processRecordingWorker) GroupClaimed(_ context.Context, tasks []models.Task) []struct {
	Tasks []models.Task
} {
	return []struct{ Tasks []models.Task }{{Tasks: tasks}}
}

func TestScheduledTaskReachesDispatchPipeline(t *testing.T) {
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.UpsertScheduledTask(context.Background(), models.ScheduledTask{
		ID: "dispatch", CronExpr: "0 * * * *", Title: "Hourly", ContextFn: "static",
		Kind: models.ScheduledTaskKindDispatch, Enabled: true, ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	sched, err := config.ParseCronExpr("0 * * * *")
	if err != nil {
		t.Fatalf("ParseCronExpr: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	s.cronByID["dispatch"] = cachedCron{expr: "0 * * * *", sched: sched}

	slot := time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC)
	if err := s.Tick(context.Background(), slot); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	tasks := store.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("ready tasks = %d, want 1", len(tasks))
	}
	if tasks[0].State != models.TaskStateReady {
		t.Fatalf("task state = %s, want READY", tasks[0].State)
	}

	rec := &processRecordingWorker{}
	claimed, err := store.ClaimNextReadyTasks(context.Background(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimNextReadyTasks: %v len=%d", err, len(claimed))
	}
	rec.Process(context.Background(), claimed[0])
	if len(rec.calls) != 1 {
		t.Fatalf("Process calls = %d, want 1", len(rec.calls))
	}
}

func TestScheduleDeferredUsesRegistry(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	runAfter := time.Now().UTC().Add(2 * time.Minute)
	if err := s.ScheduleDeferred(context.Background(), "tid", runAfter); err != nil {
		t.Fatalf("ScheduleDeferred: %v", err)
	}
	entries := store.ScheduledTasks()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Kind != models.ScheduledTaskKindRequeue {
		t.Fatalf("kind = %s, want requeue", entries[0].Kind)
	}
}
