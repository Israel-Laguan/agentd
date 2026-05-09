package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestGeneratePlanEnforcesPhaseCap(t *testing.T) {
	provider := &fakeProvider{providerName: "mock", resp: AIResponse{Content: draftPlanJSON(t, 4)}}
	router := NewRouter(provider).WithPhaseCap(3)

	plan, err := router.GeneratePlan(context.Background(), "build a large project")
	if err != nil {
		t.Fatalf("GeneratePlan() error = %v", err)
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
	if !strings.Contains(provider.resp.Content, "Task 4") {
		t.Fatal("test provider was unexpectedly mutated")
	}
}

func TestGeneratePlanAllowsCapDisabled(t *testing.T) {
	router := NewRouter(&fakeProvider{providerName: "mock", resp: AIResponse{Content: draftPlanJSON(t, 4)}}).WithPhaseCap(0)

	plan, err := router.GeneratePlan(context.Background(), "build a large project")
	if err != nil {
		t.Fatalf("GeneratePlan() error = %v", err)
	}
	if len(plan.Tasks) != 4 {
		t.Fatalf("tasks = %d, want 4", len(plan.Tasks))
	}
}

func TestGeneratePlanWithinCapPassesThrough(t *testing.T) {
	router := NewRouter(&fakeProvider{providerName: "mock", resp: AIResponse{Content: draftPlanJSON(t, 2)}}).WithPhaseCap(3)

	plan, err := router.GeneratePlan(context.Background(), "build a small project")
	if err != nil {
		t.Fatalf("GeneratePlan() error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Tasks[1].Title != "Task 2" {
		t.Fatalf("second title = %q", plan.Tasks[1].Title)
	}
}

func TestGenerateJSONDraftPlanValidationRetries(t *testing.T) {
	badPlan := `{"ProjectName":"Project","Tasks":[]}`
	goodPlan := draftPlanJSON(t, 2)
	gw := &sequenceGateway{values: []string{badPlan, goodPlan}}

	got, err := GenerateJSON[models.DraftPlan](context.Background(), gw, sampleAIRequest())
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
	if got.ProjectName != "Project" || len(got.Tasks) != 2 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(gw.requests) != 2 {
		t.Fatalf("calls = %d, want 2 (initial + retry)", len(gw.requests))
	}
}

func draftPlanJSON(t *testing.T, taskCount int) string {
	t.Helper()
	plan := models.DraftPlan{ProjectName: "Project", Description: "Large project"}
	for i := 1; i <= taskCount; i++ {
		plan.Tasks = append(plan.Tasks, models.DraftTask{
			TempID:      "task-" + string(rune('0'+i)),
			Title:       "Task " + string(rune('0'+i)),
			Description: "Do task " + string(rune('0'+i)),
		})
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return string(data)
}
