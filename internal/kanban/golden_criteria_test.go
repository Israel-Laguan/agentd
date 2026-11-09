package kanban

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestGoldenCriteriaSurvivePlanMaterialization(t *testing.T) {
	var plan models.DraftPlan
	const input = `{"project_name":"golden","tasks":[{"title":"implement","success_criteria":["tests pass"],"golden_criteria":[{"criterion":"tests pass","check_command":"go test ./..."}]}]}`
	if err := json.Unmarshal([]byte(input), &plan); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t)
	_, tasks, err := store.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetTask(context.Background(), tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Golden []struct {
			Criterion string `json:"criterion"`
			Command   string `json:"check_command"`
		} `json:"golden_criteria"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Golden) != 1 || got.Golden[0].Criterion != "tests pass" || got.Golden[0].Command != "go test ./..." {
		t.Fatalf("stored golden criteria = %+v, want tests pass checked by go test ./...", got.Golden)
	}
}

func seedTaskWithHistory(t *testing.T, store *Store, ctx context.Context) models.Task {
	t.Helper()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "golden-update", Tasks: []models.DraftTask{{Title: "task"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	if err := store.AppendEvent(ctx, models.Event{ProjectID: task.ProjectID, TaskID: sql.NullString{String: task.ID, Valid: true}, Type: "GOLDEN_CHECK_FAILURE", Payload: `{"cycle":2}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE tasks SET criteria_met='["tests pass"]' WHERE id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	seeded, err := store.GetTask(ctx, task.ID)
	if err != nil || len(seeded.CriteriaMet) != 1 {
		t.Fatalf("seeded task = %+v, error = %v", seeded, err)
	}
	return *seeded
}

func TestUpdateTaskCriteriaResetsHistory(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	task := seedTaskWithHistory(t, store, ctx)
	if _, err := store.UpdateTaskCriteria(ctx, task.ID, task.UpdatedAt.Add(-time.Second), []string{"tests pass"}, nil); !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("stale update: %v", err)
	}
	updated, err := store.UpdateTaskCriteria(ctx, task.ID, task.UpdatedAt, []string{"tests pass"}, []models.GoldenCriterion{{Criterion: "tests pass", CheckCommand: "true"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.CriteriaMet) != 0 || !updated.UpdatedAt.After(task.UpdatedAt) {
		t.Fatalf("criteria_met = %v, want reset", updated.CriteriaMet)
	}
	events, err := store.ListEventsByTask(ctx, task.ID)
	if err != nil || len(events) < 2 || events[len(events)-1].Type != "GOLDEN_CRITERIA_RESET" {
		t.Fatalf("events = %+v, error = %v", events, err)
	}
}

func TestUpdateTaskCriteriaRejectsRunning(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	task := seedTaskWithHistory(t, store, ctx)
	updated, err := store.UpdateTaskCriteria(ctx, task.ID, task.UpdatedAt, []string{"tests pass"}, []models.GoldenCriterion{{Criterion: "tests pass", CheckCommand: "true"}})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim task: %+v, %v", claimed, err)
	}
	running, err := store.MarkTaskRunning(ctx, updated.ID, claimed[0].UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateTaskCriteria(ctx, running.ID, running.UpdatedAt, nil, nil); !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("update running task: %v", err)
	}
	fresh, err := store.GetTask(ctx, task.ID)
	if err != nil || len(fresh.GoldenCriteria) != 1 {
		t.Fatalf("stored task = %+v, error = %v", fresh, err)
	}
}
