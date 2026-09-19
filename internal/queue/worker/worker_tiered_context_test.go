package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func tieredContextPack(parentID string) *ContextPack {
	cp := NewContextPack("ctx-task", parentID, "gathered context", []string{"a.go"}, ContextPackConfig{MaxPaths: 40, MaxChars: 48000})
	return cp
}

func TestInjectContextPack_UsesTaskScopedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pack produced by pipeline A (origin task "parent-a").
	if err := WriteContextPack(dir, tieredContextPack("parent-a")); err != nil {
		t.Fatalf("WriteContextPack: %v", err)
	}
	// Pack produced by pipeline B in the same workspace.
	if err := WriteContextPack(dir, tieredContextPack("parent-b")); err != nil {
		t.Fatalf("WriteContextPack: %v", err)
	}

	w := &Worker{}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "decision-1"}, Description: "do the thing"}
	project := models.Project{WorkspacePath: dir}

	// Each pipeline must resolve its own pack, not the single shared file.
	for _, origin := range []string{"parent-a", "parent-b"} {
		got := w.injectContextPack(task, models.Task{BaseEntity: models.BaseEntity{ID: origin}}, project)
		if !strings.HasPrefix(got.Description, "CONTEXT PACK") {
			t.Fatalf("injectContextPack(origin=%s) did not inject a pack: %q", origin, got.Description)
		}
		if !strings.Contains(got.Description, "Original task:\ndo the thing") {
			t.Fatalf("injectContextPack(origin=%s) lost the original description: %q", origin, got.Description)
		}
	}
}

func TestInjectContextPack_RejectsForeignPackLineage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A stale pack scoped to parent-a but carrying pipeline B's lineage.
	pack := tieredContextPack("parent-b")
	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	packPath := filepath.Join(dir, PackFilePath("parent-a", ContextPackVersion))
	if err := os.WriteFile(packPath, data, 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	w := &Worker{}
	got := w.injectContextPack(
		models.Task{BaseEntity: models.BaseEntity{ID: "decision-1"}, Description: "original"},
		models.Task{BaseEntity: models.BaseEntity{ID: "parent-a"}},
		models.Project{WorkspacePath: dir},
	)
	if got.Description != "original" {
		t.Fatalf("injectContextPack injected a foreign pack: %q", got.Description)
	}
}

func TestProcessTieredContextStep_FailsWhenProviderLacksChatTools(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	// A bare Worker has no chat-tools-aware gateway, so the tiered context
	// step must fail instead of silently completing via legacy fallback.
	w := &Worker{store: store, sink: sink}

	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tiered-fixture",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	*task = models.Task{BaseEntity: task.BaseEntity, ProjectID: task.ProjectID, AgentID: tieredStepProfile[TieredStepContext], State: task.State}
	project := models.Project{WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{Provider: "synth-no-tools", AgenticMode: true}

	w.processTieredContextStep(ctx, *task, project, profile, models.Task{BaseEntity: models.BaseEntity{ID: "parent-1"}})

	got, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailed {
		t.Fatalf("state = %s, want FAILED (step must not complete without a pack)", got.State)
	}
	var sawProviderEvent bool
	for _, ev := range sink.events {
		if ev.Type == "TIERED_CONTEXT_PROVIDER_UNSUPPORTED" {
			sawProviderEvent = true
		}
	}
	if !sawProviderEvent {
		t.Fatalf("expected TIERED_CONTEXT_PROVIDER_UNSUPPORTED event, got %+v", sink.events)
	}
}

func TestFailTieredStep_SurfacesStateConflict(t *testing.T) {
	t.Parallel()
	base := testutil.NewFakeStore()
	store := &conflictingResultStore{FakeKanbanStore: base}
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	ctx := context.Background()
	task := models.Task{BaseEntity: models.BaseEntity{ID: "ctx-1"}, ProjectID: "proj"}

	w.failTieredStep(ctx, task, "invalid ContextPack")

	var found bool
	for _, ev := range sink.events {
		if ev.Type == "TIERED_STEP_FAIL_CONFLICT" && strings.Contains(ev.Payload, "invalid ContextPack") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TIERED_STEP_FAIL_CONFLICT escalation event, got %+v", sink.events)
	}
}

// conflictingResultStore always reports an optimistic-lock conflict on
// UpdateTaskResult, simulating a task that left RUNNING before the failure
// write (e.g. a concurrent commit or heartbeat bump).
type conflictingResultStore struct {
	*testutil.FakeKanbanStore
}

func (s *conflictingResultStore) UpdateTaskResult(ctx context.Context, id string, expected time.Time, result models.TaskResult) (*models.Task, error) {
	return nil, models.ErrStateConflict
}
