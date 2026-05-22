package queue

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/memory"
	"agentd/internal/models"
	qw "agentd/internal/queue/worker"
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

func TestSchedulerDescriptorEveryFiveMinutes(t *testing.T) {
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	entry := models.ScheduledTask{
		ID:          "health",
		CronExpr:    "@every 5m",
		Title:       "Health",
		ContextFn:   "static",
		ContextArgs: map[string]string{"body": "check"},
		Kind:        models.ScheduledTaskKindDispatch,
		Enabled:     true,
		ProjectID:   project.ID,
		CreatedAt:   base,
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

	if err := s.Tick(context.Background(), base); err != nil {
		t.Fatalf("Tick at :00: %v", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("tasks after first tick = %d, want 0 (@every waits one interval)", len(store.Tasks()))
	}
	if err := s.Tick(context.Background(), base.Add(2*time.Minute)); err != nil {
		t.Fatalf("Tick at :02: %v", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("tasks after :02 tick = %d, want 0", len(store.Tasks()))
	}
	if err := s.Tick(context.Background(), base.Add(5*time.Minute)); err != nil {
		t.Fatalf("Tick at :05: %v", err)
	}
	if len(store.Tasks()) != 1 {
		t.Fatalf("tasks after :05 tick = %d, want 1", len(store.Tasks()))
	}
	if err := s.Tick(context.Background(), base.Add(10*time.Minute)); err != nil {
		t.Fatalf("Tick at :10: %v", err)
	}
	if len(store.Tasks()) != 2 {
		t.Fatalf("tasks after :10 tick = %d, want 2", len(store.Tasks()))
	}
}

func TestSchedulerEveryWaitsFullIntervalFromSubMinuteCreatedAt(t *testing.T) {
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	createdAt := base.Add(30 * time.Second)
	entry := models.ScheduledTask{
		ID:          "health",
		CronExpr:    "@every 5m",
		Title:       "Health",
		ContextFn:   "static",
		ContextArgs: map[string]string{"body": "check"},
		Kind:        models.ScheduledTaskKindDispatch,
		Enabled:     true,
		ProjectID:   project.ID,
		CreatedAt:   createdAt,
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

	if err := s.Tick(context.Background(), base.Add(5*time.Minute)); err != nil {
		t.Fatalf("Tick at :05: %v", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("tasks at :05 = %d, want 0 (interval not elapsed from :00:30)", len(store.Tasks()))
	}
	if err := s.Tick(context.Background(), base.Add(6*time.Minute)); err != nil {
		t.Fatalf("Tick at :06: %v", err)
	}
	if len(store.Tasks()) != 1 {
		t.Fatalf("tasks at :06 = %d, want 1", len(store.Tasks()))
	}
}

func TestSchedulerEveryWaitsIntervalCalendarFiresAtBoundary(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)

	entries := []models.ScheduledTask{
		{
			ID: "calendar", CronExpr: "*/5 * * * *", Title: "Calendar",
			ContextFn: "static", Kind: models.ScheduledTaskKindDispatch,
			Enabled: true, ProjectID: project.ID, CreatedAt: base,
		},
		{
			ID: "every", CronExpr: "@every 5m", Title: "Every",
			ContextFn: "static", Kind: models.ScheduledTaskKindDispatch,
			Enabled: true, ProjectID: project.ID, CreatedAt: base,
		},
	}
	for _, entry := range entries {
		if err := store.UpsertScheduledTask(ctx, entry); err != nil {
			t.Fatalf("UpsertScheduledTask %s: %v", entry.ID, err)
		}
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	for _, entry := range entries {
		sched, err := config.ParseCronExpr(entry.CronExpr)
		if err != nil {
			t.Fatalf("ParseCronExpr %s: %v", entry.ID, err)
		}
		s.cronByID[entry.ID] = cachedCron{expr: entry.CronExpr, sched: sched}
	}
	if err := s.Tick(ctx, base); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(store.Tasks()) != 1 {
		t.Fatalf("tasks at :00 = %d, want 1 (calendar only)", len(store.Tasks()))
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

func TestScheduleDeferredRequeueClearsStaleLastFired(t *testing.T) {
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

	staleFired := time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC)
	oldRunAfter := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID:           "defer:" + task.ID,
		RunAfter:     &oldRunAfter,
		Kind:         models.ScheduledTaskKindRequeue,
		TargetTaskID: task.ID,
		Enabled:      true,
		LastFiredAt:  &staleFired,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask stale entry: %v", err)
	}

	newRunAfter := time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC)
	if err := store.ScheduleDeferredRequeue(ctx, task.ID, newRunAfter); err != nil {
		t.Fatalf("ScheduleDeferredRequeue: %v", err)
	}
	entries := store.ScheduledTasks()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].LastFiredAt != nil {
		t.Fatalf("LastFiredAt = %v, want nil after reschedule", entries[0].LastFiredAt)
	}

	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	if err := s.Tick(ctx, newRunAfter); err != nil {
		t.Fatalf("Tick at run_after: %v", err)
	}
	got, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", got.State)
	}
	if len(store.ScheduledTasks()) != 0 {
		t.Fatalf("scheduled entries = %d, want 0", len(store.ScheduledTasks()))
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

func TestBootstrapFromConfigSkipsEmptyID(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	if err := s.BootstrapFromConfig(context.Background(), config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{ID: "  ", CronExpr: "0 * * * *", ContextFn: "static"},
			{ID: "ok", CronExpr: "0 * * * *", ContextFn: "static"},
		},
	}); err != nil {
		t.Fatalf("BootstrapFromConfig: %v", err)
	}
	if len(store.ScheduledTasks()) != 1 {
		t.Fatalf("scheduled entries = %d, want 1", len(store.ScheduledTasks()))
	}
}

func TestBootstrapFromConfigUpsertsTasks(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})

	runAfter := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cfg := config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{
				ID:          "cron-job",
				CronExpr:    "0 * * * *",
				TaskType:    "summarize",
				ContextFn:   "static",
				ContextArgs: map[string]string{"body": "cron body"},
				Title:       "Cron Job",
			},
			{
				ID:        "once-job",
				RunAfter:  runAfter.Format(time.RFC3339),
				ContextFn: "static",
				ContextArgs: map[string]string{"body": "once body"},
				Title:     "Once Job",
			},
		},
	}
	if err := s.BootstrapFromConfig(ctx, cfg); err != nil {
		t.Fatalf("BootstrapFromConfig: %v", err)
	}
	entries := store.ScheduledTasks()
	if len(entries) != 2 {
		t.Fatalf("scheduled entries = %d, want 2", len(entries))
	}
	if _, ok := s.cronByID["cron-job"]; !ok {
		t.Fatal("cronByID missing cron-job")
	}
	if _, ok := s.cronByID["once-job"]; ok {
		t.Fatal("cronByID should not cache one-shot without cron_expr")
	}
	for _, e := range entries {
		if e.OutputTarget != config.DefaultSchedulerOutputTarget {
			t.Fatalf("entry %q OutputTarget = %q, want %q", e.ID, e.OutputTarget, config.DefaultSchedulerOutputTarget)
		}
	}
}

func TestBootstrapFromConfigInvalidCron(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	err := s.BootstrapFromConfig(context.Background(), config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{ID: "bad", CronExpr: "not valid cron", ContextFn: "static"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cron") {
		t.Fatalf("BootstrapFromConfig() err = %v, want cron error", err)
	}
}

func TestBootstrapFromConfigInvalidRunAfter(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	err := s.BootstrapFromConfig(context.Background(), config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{ID: "bad", RunAfter: "not-a-time", ContextFn: "static"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "run_after") {
		t.Fatalf("BootstrapFromConfig() err = %v, want run_after error", err)
	}
}

func TestNewSchedulerFromConfig(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	agentic := config.AgenticConfig{
		Scheduler: config.SchedulerConfig{
			Enabled: true,
			Tasks: []config.SchedulerTaskConfig{
				{
					ID:        "boot",
					CronExpr:  "0 * * * *",
					ContextFn: "static",
					Title:     "Boot",
				},
			},
		},
	}
	s, err := NewSchedulerFromConfig(ctx, store, nil, agentic, config.LibrarianConfig{})
	if err != nil {
		t.Fatalf("NewSchedulerFromConfig: %v", err)
	}
	if s == nil || !s.Enabled() {
		t.Fatal("expected enabled scheduler")
	}
	if len(store.ScheduledTasks()) != 1 {
		t.Fatalf("scheduled tasks = %d, want 1", len(store.ScheduledTasks()))
	}
}

func TestNewSchedulerFromConfigRequiresScheduledStore(t *testing.T) {
	_, err := NewSchedulerFromConfig(
		context.Background(),
		nil,
		nil,
		config.AgenticConfig{Scheduler: config.SchedulerConfig{Enabled: true}},
		config.LibrarianConfig{},
	)
	if err == nil || !strings.Contains(err.Error(), "ScheduledTaskStore") {
		t.Fatalf("NewSchedulerFromConfig() err = %v, want ScheduledTaskStore error", err)
	}
}

func TestStaticContextProviderFetch(t *testing.T) {
	p := staticContextProvider{}
	body, err := p.Fetch(context.Background(), models.ScheduledTask{
		ContextArgs: map[string]string{"body": "hello"},
	}, "")
	if err != nil || body != "hello" {
		t.Fatalf("Fetch body = %q err = %v", body, err)
	}
	text, err := p.Fetch(context.Background(), models.ScheduledTask{
		ContextArgs: map[string]string{"text": "fallback"},
	}, "")
	if err != nil || text != "fallback" {
		t.Fatalf("Fetch text = %q err = %v", text, err)
	}
}

func TestMemoryRecallProviderUsesTitleWhenIntentMissing(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.RecordMemory(ctx, models.Memory{
		ID: "m1", Scope: "GLOBAL",
		Symptom: sql.NullString{String: "symptom", Valid: true},
		Solution: sql.NullString{String: "fix", Valid: true},
	}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	p := memoryRecallProvider{retriever: &memory.Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: time.Second}}}
	body, err := p.Fetch(ctx, models.ScheduledTask{Title: "My Title"}, project.ID)
	if err != nil || !strings.Contains(body, "symptom") {
		t.Fatalf("Fetch body = %q err = %v", body, err)
	}
}

func TestMemoryRecallProviderFetch(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.RecordMemory(ctx, models.Memory{
		ID: "m1", Scope: "GLOBAL",
		Symptom: sql.NullString{String: "slow build", Valid: true},
		Solution: sql.NullString{String: "use cache", Valid: true},
	}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	providers := defaultContextProviders(store, config.LibrarianConfig{RecallTimeout: time.Second, RecallTopK: 5})
	p, ok := providers["memory_recall"].(memoryRecallProvider)
	if !ok {
		t.Fatal("memory_recall provider missing")
	}
	body, err := p.Fetch(ctx, models.ScheduledTask{
		ContextArgs: map[string]string{"intent": "build"},
		Title:       "Recall",
	}, project.ID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(body, "LESSONS LEARNED") || !strings.Contains(body, "slow build") {
		t.Fatalf("Fetch body = %q, want lesson content", body)
	}
}

func TestHouseRulesProviderNilStore(t *testing.T) {
	p := houseRulesProvider{}
	body, err := p.Fetch(context.Background(), models.ScheduledTask{}, "")
	if err != nil || body != "" {
		t.Fatalf("Fetch body = %q err = %v, want empty", body, err)
	}
}

func TestHouseRulesProviderFetch(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	if err := store.SetSetting(ctx, models.SettingKeyHouseRules, "POSIX sh only."); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	providers := defaultContextProviders(store, config.LibrarianConfig{})
	p, ok := providers["house_rules"].(houseRulesProvider)
	if !ok {
		t.Fatal("house_rules provider missing")
	}
	body, err := p.Fetch(ctx, models.ScheduledTask{}, "")
	if err != nil || body != "POSIX sh only." {
		t.Fatalf("Fetch body = %q err = %v", body, err)
	}
}

func TestSchedulerUnknownContextFn(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID: "bad-ctx", CronExpr: "0 * * * *", Title: "Bad",
		ContextFn: "missing", Kind: models.ScheduledTaskKindDispatch,
		Enabled: true, ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	sched, err := config.ParseCronExpr("0 * * * *")
	if err != nil {
		t.Fatalf("ParseCronExpr: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	s.cronByID["bad-ctx"] = cachedCron{expr: "0 * * * *", sched: sched}

	slot := time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC)
	if err := s.Tick(ctx, slot); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("tasks = %d, want 0 on unknown context_fn", len(store.Tasks()))
	}
}

func TestTaskTypeKeywords(t *testing.T) {
	cases := map[string]string{
		qw.TaskTypeSummarize:    "summarize summary recap",
		qw.TaskTypeCodeGen:      "implement fix refactor code",
		qw.TaskTypeDocQA:        "explain document readme",
		qw.TaskTypeWebResearch:  "search fetch web url",
		qw.TaskTypeFullAgent:    "orchestrate multi-step deploy",
		"custom_type":           "custom_type",
	}
	for taskType, want := range cases {
		if got := taskTypeKeywords(taskType); got != want {
			t.Fatalf("taskTypeKeywords(%q) = %q, want %q", taskType, got, want)
		}
	}
}

func TestBuildScheduledDescription(t *testing.T) {
	entry := models.ScheduledTask{
		DescriptionTemplate: "Template line",
		TaskType:            qw.TaskTypeSummarize,
	}
	got := buildScheduledDescription(entry, "context body")
	for _, want := range []string{"Template line", "context body", "Scheduled task type:", "summarize summary recap"} {
		if !strings.Contains(got, want) {
			t.Fatalf("description missing %q:\n%s", want, got)
		}
	}
	if got := buildScheduledDescription(models.ScheduledTask{}, ""); got != "" {
		t.Fatalf("empty entry description = %q, want empty", got)
	}
}

func TestNormalizeOutputTarget(t *testing.T) {
	cases := map[string]models.EventType{
		"RESULT": models.EventTypeResult,
		"result": models.EventTypeResult,
		"LOG":    models.EventTypeLog,
		"":       models.EventTypeLog,
		"CUSTOM": models.EventType("CUSTOM"),
	}
	for in, want := range cases {
		if got := normalizeOutputTarget(in); got != want {
			t.Fatalf("normalizeOutputTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSchedulerEmitDispatchEvent(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID: "evt", CronExpr: "0 * * * *", Title: "Event",
		ContextFn: "static", OutputTarget: "RESULT",
		Kind: models.ScheduledTaskKindDispatch, Enabled: true, ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	sched, err := config.ParseCronExpr("0 * * * *")
	if err != nil {
		t.Fatalf("ParseCronExpr: %v", err)
	}
	sink := &recordingSink{}
	s := NewScheduler(store, sink, SchedulerOptions{Enabled: true})
	s.cronByID["evt"] = cachedCron{expr: "0 * * * *", sched: sched}

	slot := time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC)
	if err := s.Tick(ctx, slot); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Type != models.EventTypeResult {
		t.Fatalf("event type = %q, want RESULT", ev.Type)
	}
	if !strings.Contains(ev.Payload, "scheduled_id=evt") {
		t.Fatalf("payload = %q, want scheduled_id", ev.Payload)
	}
}

func TestSchedulerResolveProjectID(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	sys, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}

	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true, ProjectID: "opt-project"})
	got, err := s.resolveProjectID(ctx, models.ScheduledTask{ProjectID: "entry-project"})
	if err != nil || got != "entry-project" {
		t.Fatalf("entry project = %q err = %v", got, err)
	}
	got, err = s.resolveProjectID(ctx, models.ScheduledTask{})
	if err != nil || got != "opt-project" {
		t.Fatalf("options project = %q err = %v", got, err)
	}

	s2 := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	got, err = s2.resolveProjectID(ctx, models.ScheduledTask{})
	if err != nil || got != sys.ID {
		t.Fatalf("system project = %q err = %v, want %s", got, err, sys.ID)
	}
}

func TestSchedulerEnabled(t *testing.T) {
	store := testutil.NewFakeStore()
	disabled := NewScheduler(store, nil, SchedulerOptions{Enabled: false})
	if disabled.Enabled() {
		t.Fatal("expected disabled scheduler")
	}
	if err := disabled.Tick(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("disabled Tick: %v", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("disabled Tick created tasks = %d", len(store.Tasks()))
	}
	if NewScheduler(nil, nil, SchedulerOptions{}).Enabled() {
		t.Fatal("nil scheduler should not be enabled")
	}
}

func TestSchedulerCronDueInvalidExpr(t *testing.T) {
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.UpsertScheduledTask(context.Background(), models.ScheduledTask{
		ID: "bad-cron", CronExpr: "invalid cron", Title: "Bad",
		ContextFn: "static", Kind: models.ScheduledTaskKindDispatch,
		Enabled: true, ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	slot := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	if err := s.Tick(context.Background(), slot); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("tasks = %d, want 0 for invalid cron", len(store.Tasks()))
	}
}

func TestFireRequeueDeletesOrphanEntry(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	slot := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID: "orphan", Kind: models.ScheduledTaskKindRequeue, Enabled: true,
		RunAfter: &slot,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask empty target: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	if err := s.Tick(ctx, slot); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(store.ScheduledTasks()) != 0 {
		t.Fatalf("scheduled entries = %d, want 0", len(store.ScheduledTasks()))
	}
}

func TestFireRequeueTaskNotFound(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	runAfter := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID: "defer:missing", RunAfter: &runAfter,
		Kind: models.ScheduledTaskKindRequeue, TargetTaskID: "missing",
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertScheduledTask: %v", err)
	}
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	if err := s.Tick(ctx, runAfter); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(store.ScheduledTasks()) != 0 {
		t.Fatalf("scheduled entries = %d, want 0 after missing task", len(store.ScheduledTasks()))
	}
}

func TestMemoryRecallProviderNilRetriever(t *testing.T) {
	p := memoryRecallProvider{}
	body, err := p.Fetch(context.Background(), models.ScheduledTask{Title: "x"}, "p1")
	if err != nil || body != "" {
		t.Fatalf("Fetch = %q err = %v, want empty", body, err)
	}
}

func TestMemoryRecallProviderPreferences(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	if err := store.RecordMemory(ctx, models.Memory{
		ID: "pref1", Scope: "USER_PREFERENCE",
		Solution: sql.NullString{String: "be concise", Valid: true},
	}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	p := memoryRecallProvider{retriever: &memory.Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: time.Second}}}
	body, err := p.Fetch(ctx, models.ScheduledTask{ContextArgs: map[string]string{"user_id": "u1"}}, "p1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(body, "USER PREFERENCES") || !strings.Contains(body, "be concise") {
		t.Fatalf("Fetch body = %q, want preferences", body)
	}
}
