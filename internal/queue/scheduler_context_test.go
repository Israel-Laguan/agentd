package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
	qw "agentd/internal/queue/worker"
	"agentd/internal/testutil"
)

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
		qw.TaskTypeSummarize:   "summarize summary recap",
		qw.TaskTypeCodeGen:     "implement fix refactor code",
		qw.TaskTypeDocQA:       "explain document readme",
		qw.TaskTypeWebResearch: "search fetch web url",
		qw.TaskTypeFullAgent:   "orchestrate multi-step deploy",
		"custom_type":          "custom_type",
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
