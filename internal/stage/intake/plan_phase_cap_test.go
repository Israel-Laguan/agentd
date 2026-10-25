package intake

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

func TestPlanEnforcesPhaseCap(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{mustDraftPlanJSON(t, 4)}}

	plan, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 3, "build a large project")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(plan.Tasks))
	}
	last := plan.Tasks[2]
	if last.Title != "Plan Phase 2" {
		t.Fatalf("last title = %q, want Plan Phase 2", last.Title)
	}
	if !strings.Contains(last.Description, "Task 3") || !strings.Contains(last.Description, "Task 4") {
		t.Fatalf("continuation description = %q", last.Description)
	}
	if !strings.Contains(gw.replies[0], "Task 4") {
		t.Fatal("test reply was unexpectedly mutated")
	}
}

func TestPlanAllowsCapDisabled(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{mustDraftPlanJSON(t, 4)}}

	plan, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 0, "build a large project")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Tasks) != 4 {
		t.Fatalf("tasks = %d, want 4", len(plan.Tasks))
	}
}

func TestPlanWithinCapPassesThrough(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{mustDraftPlanJSON(t, 2)}}

	plan, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 3, "build a small project")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Tasks[1].Title != "Task 2" {
		t.Fatalf("second title = %q", plan.Tasks[1].Title)
	}
}

// T-057: the plan call carries the task the caller put on ctx (intake replans,
// next-phase planning), so the call record names it.
func TestPlanCarriesTheContextTaskID(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{`{"project_name":"P","tasks":[{"title":"a","description":"d","assignee":"SYSTEM"}]}`}}
	ctx := spec.WithTaskID(context.Background(), "task-9")

	if _, err := Plan(ctx, stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 0, "do it"); err != nil {
		t.Fatal(err)
	}
	if len(gw.seen) != 1 || gw.seen[0].TaskID != "task-9" {
		t.Fatalf("requests = %+v, want one request with TaskID task-9", gw.seen)
	}
}

// draftPlanJSON is a DraftPlan with n tasks titled "Task 1".."Task n".
func draftPlanJSON(n int) (string, error) {
	plan := models.DraftPlan{ProjectName: "Project", Description: "Large project"}
	for i := 1; i <= n; i++ {
		plan.Tasks = append(plan.Tasks, models.DraftTask{
			TempID:      "task-" + string(rune('0'+i)),
			Title:       "Task " + string(rune('0'+i)),
			Description: "Do task " + string(rune('0'+i)),
		})
	}
	data, err := json.Marshal(plan)
	return string(data), err
}

func mustDraftPlanJSON(t *testing.T, n int) string {
	t.Helper()
	raw, err := draftPlanJSON(n)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return raw
}
