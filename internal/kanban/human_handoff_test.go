package kanban

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestResolveHumanHandoffCompletesChildAndParent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	parent := seedTestTask(t, store, "human-resolution", models.TaskStateRunning)
	_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualAction + " privileged command",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}
	child := children[0]
	expected := child.UpdatedAt
	resolution, err := store.ResolveHumanHandoff(ctx, child.ID, &expected, "api_key=supersecretvalue host output")
	if err != nil {
		t.Fatalf("ResolveHumanHandoff: %v", err)
	}
	if resolution.Task.State != models.TaskStateCompleted || resolution.Parent.State != models.TaskStateCompleted {
		t.Fatalf("resolution states = %s / %s", resolution.Task.State, resolution.Parent.State)
	}
	if strings.Contains(resolution.Result, "supersecretvalue") || !strings.Contains(resolution.Result, "[REDACTED]") {
		t.Fatalf("resolution result was not redacted: %q", resolution.Result)
	}
	events, err := store.ListEventsByTask(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask(child): %v", err)
	}
	if !hasEventType(events, models.EventTypeHumanResolution) {
		t.Fatalf("child events = %#v, want HUMAN_RESOLUTION", events)
	}
	parentEvents, err := store.ListEventsByTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask(parent): %v", err)
	}
	if !hasEventType(parentEvents, models.EventTypeResult) {
		t.Fatalf("parent events = %#v, want RESULT", parentEvents)
	}
}

func TestResolveHumanHandoffRejectsStaleDuplicateAndOpenSibling(t *testing.T) {
	t.Run("stale version", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()
		parent := seedTestTask(t, store, "stale", models.TaskStateRunning)
		_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
			Title: models.HITLSubtaskTitleManualAction + " privileged command", Assignee: models.TaskAssigneeHuman,
		}})
		if err != nil {
			t.Fatal(err)
		}
		stale := children[0].UpdatedAt.Add(-time.Second)
		_, err = store.ResolveHumanHandoff(ctx, children[0].ID, &stale, "done")
		if !errors.Is(err, models.ErrOptimisticLock) {
			t.Fatalf("error = %v, want ErrOptimisticLock", err)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()
		parent := seedTestTask(t, store, "duplicate", models.TaskStateRunning)
		_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
			Title: models.HITLSubtaskTitleManualAction + " privileged command", Assignee: models.TaskAssigneeHuman,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ResolveHumanHandoff(ctx, children[0].ID, nil, "done"); err != nil {
			t.Fatal(err)
		}
		_, err = store.ResolveHumanHandoff(ctx, children[0].ID, nil, "again")
		if !errors.Is(err, models.ErrStateConflict) {
			t.Fatalf("error = %v, want ErrStateConflict", err)
		}
	})

	t.Run("other open child", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()
		parent := seedTestTask(t, store, "siblings", models.TaskStateRunning)
		_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{
			{Title: models.HITLSubtaskTitleManualAction + " privileged command", Assignee: models.TaskAssigneeHuman},
			{Title: "second child", Assignee: models.TaskAssigneeHuman},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.ResolveHumanHandoff(ctx, children[0].ID, nil, "done")
		if !errors.Is(err, models.ErrStateConflict) {
			t.Fatalf("error = %v, want ErrStateConflict", err)
		}
	})
}

func TestResolveHumanHandoffRejectsOrphanAndEmptyResult(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	orphan := seedTestTask(t, store, "orphan", models.TaskStateReady)
	if _, err := store.ResolveHumanHandoff(ctx, orphan.ID, nil, "done"); !errors.Is(err, models.ErrHumanHandoffInvalid) {
		t.Fatalf("orphan error = %v, want ErrHumanHandoffInvalid", err)
	}
	if _, err := store.ResolveHumanHandoff(ctx, orphan.ID, nil, " "); !errors.Is(err, models.ErrHumanHandoffInvalid) {
		t.Fatalf("empty error = %v, want ErrHumanHandoffInvalid", err)
	}
}

func hasEventType(events []models.Event, eventType models.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
