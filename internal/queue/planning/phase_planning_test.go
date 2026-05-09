package planning

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestIsPhasePlanningTask(t *testing.T) {
	cases := map[string]bool{
		"Plan Phase 2":      true,
		" plan phase 12 ":   true,
		"Plan phase two":    false,
		"Implement Phase 2": false,
	}
	for title, want := range cases {
		if got := IsPhasePlanningTask(title); got != want {
			t.Fatalf("IsPhasePlanningTask(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestNextPhaseNumber(t *testing.T) {
	if got := NextPhaseNumber("Plan Phase 2"); got != 3 {
		t.Fatalf("nextPhaseNumber() = %d, want 3", got)
	}
	if got := NextPhaseNumber("not a phase task"); got != 2 {
		t.Fatalf("nextPhaseNumber(invalid) = %d, want 2", got)
	}
}

func TestBuildPhaseIntent(t *testing.T) {
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "planning"},
		Title:       "Plan Phase 2",
		Description: "Finish backend and frontend.",
	}
	project := models.Project{Name: "Website", OriginalInput: "Build a website"}
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "done"}, Title: "Set up repo", Description: "Initialize app", State: models.TaskStateCompleted},
		task,
	}

	intent := BuildPhaseIntent(task, project, tasks)
	for _, want := range []string{"Website", "Build a website", "Finish backend and frontend.", "[COMPLETED] Set up repo", "Plan Phase 3"} {
		if !strings.Contains(intent, want) {
			t.Fatalf("intent missing %q:\n%s", want, intent)
		}
	}
	if strings.Contains(intent, "[") && strings.Contains(intent, "Plan Phase 2: Finish") {
		t.Fatalf("intent included current planning task as existing task:\n%s", intent)
	}
}

func TestRetitlePhaseContinuationTasks(t *testing.T) {
	tasks := []models.DraftTask{
		{Title: "Build API"},
		{Title: "Plan Phase 2", Description: "Continue"},
	}
	retitled := RetitlePhaseContinuationTasks(tasks, 3)

	if retitled[1].Title != "Plan Phase 3" {
		t.Fatalf("retitled continuation = %q, want Plan Phase 3", retitled[1].Title)
	}
	if tasks[1].Title != "Plan Phase 2" {
		t.Fatalf("original task was mutated: %#v", tasks)
	}
}
