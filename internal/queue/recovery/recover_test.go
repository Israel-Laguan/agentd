package recovery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
	"agentd/internal/testutil"
)

type recordingSink struct {
	events []models.Event
	errOn  int
}

func (s *recordingSink) Emit(_ context.Context, event models.Event) error {
	s.events = append(s.events, event)
	if s.errOn > 0 && len(s.events) == s.errOn {
		return errors.New("emit failed")
	}
	return nil
}

type errProbe struct{}

func (errProbe) AlivePIDs(context.Context) ([]int, error) {
	return nil, errors.New("probe failed")
}

func TestEmitHeartbeatReconcile_nilSink(t *testing.T) {
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1"}
	if err := EmitHeartbeatReconcile(context.Background(), nil, []models.Task{task}); err != nil {
		t.Fatalf("EmitHeartbeatReconcile() error = %v", err)
	}
}

func TestEmitHeartbeatReconcile_multiTask(t *testing.T) {
	sink := &recordingSink{}
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1"},
		{BaseEntity: models.BaseEntity{ID: "t2"}, ProjectID: "p1"},
	}
	if err := EmitHeartbeatReconcile(context.Background(), sink, tasks); err != nil {
		t.Fatalf("EmitHeartbeatReconcile() error = %v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Type != HeartbeatReconcileEventType {
		t.Fatalf("event type = %s", sink.events[0].Type)
	}
}

func TestEmitHeartbeatReconcile_emitError(t *testing.T) {
	sink := &recordingSink{errOn: 1}
	err := EmitHeartbeatReconcile(context.Background(), sink, []models.Task{{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1"}})
	if err == nil || !strings.Contains(err.Error(), "emit heartbeat reconcile") {
		t.Fatalf("EmitHeartbeatReconcile() error = %v", err)
	}
}

func TestBootReconcile_noGhosts(t *testing.T) {
	store := testutil.NewFakeStore()
	sink := &recordingSink{}
	if err := BootReconcile(context.Background(), store, safety.StaticPIDProbe{PIDs: []int{1}}, sink); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %#v, want none", sink.events)
	}
}

func TestBootReconcile_probeError(t *testing.T) {
	store := testutil.NewFakeStore()
	err := BootReconcile(context.Background(), store, errProbe{}, nil)
	if err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("BootReconcile() error = %v", err)
	}
}

func TestBootReconcile_handoffSkippedWhenTaskExists(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()
	taskID := seedRunningGhostTask(t, ctx, store)

	system, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	_, _, err = store.EnsureProjectTask(ctx, system.ID, models.DraftTask{
		Title:       "Daemon Reboot Recovery: 1 task(s) interrupted",
		Description: "existing",
		Assignee:    models.TaskAssigneeHuman,
	})
	if err != nil {
		t.Fatalf("EnsureProjectTask: %v", err)
	}

	sink := &recordingSink{}
	if err := BootReconcile(ctx, store, safety.StaticPIDProbe{PIDs: []int{1}}, sink); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}
	for _, ev := range sink.events {
		if ev.Type == RebootRecoveryHandoffEventType {
			t.Fatalf("unexpected handoff event: %#v", ev)
		}
	}
	_ = taskID
}

func TestRebootRecoveryDescription(t *testing.T) {
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "a"}, Title: "Task A", ProjectID: "p1"},
		{BaseEntity: models.BaseEntity{ID: "b"}, Title: "Task B", ProjectID: "p2"},
	}
	desc := rebootRecoveryDescription(tasks, time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{"agentd recovered 2 task(s)", "Task A", "Task B", "partial filesystem"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
	}

	payload := rebootRecoveryPayload(tasks, time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC))
	if !strings.Contains(payload, "task_count=2") || !strings.Contains(payload, `title="Task A"`) {
		t.Fatalf("payload = %q", payload)
	}
}

func seedRunningGhostTask(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore) string {
	t.Helper()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "recover",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "Interrupted"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks: %v", err)
	}
	if _, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 9999); err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	return tasks[0].ID
}
