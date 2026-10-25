package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"agentd/internal/models"
)

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
