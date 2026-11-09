package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestTaskHandler_ListByProject_IncludesCriteriaMet(t *testing.T) {
	h, store := taskTestHandler()
	ctx := context.Background()
	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "criteria-project",
		Tasks: []models.DraftTask{{
			Title:           "Report task",
			Description:     "Write REPORT.md",
			SuccessCriteria: []string{"REPORT.md exists", "REPORT.md contains tree"},
		}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	taskID := tasks[0].ID
	if _, err := store.MarkTaskRunning(ctx, taskID, tasks[0].UpdatedAt, 4242); err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	if err := store.UpdateCriteriaMet(ctx, taskID, []string{"REPORT.md exists"}); err != nil {
		t.Fatalf("UpdateCriteriaMet: %v", err)
	}
	running, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, taskID, running.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+proj.ID+"/tasks", nil)
	req.SetPathValue("id", proj.ID)
	rec := httptest.NewRecorder()
	h.ListByProject(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListByProject code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			SuccessCriteria []string `json:"success_criteria"`
			CriteriaMet     []string `json:"criteria_met"`
			State           string   `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("task count = %d, want 1", len(resp.Data))
	}
	row := resp.Data[0]
	if len(row.SuccessCriteria) != 2 ||
		row.SuccessCriteria[0] != "REPORT.md exists" ||
		row.SuccessCriteria[1] != "REPORT.md contains tree" {
		t.Fatalf("success_criteria = %v, want [REPORT.md exists REPORT.md contains tree]", row.SuccessCriteria)
	}
	if len(row.CriteriaMet) != 1 || row.CriteriaMet[0] != "REPORT.md exists" {
		t.Fatalf("criteria_met = %v, want [REPORT.md exists]", row.CriteriaMet)
	}
	if row.State != string(models.TaskStateCompleted) {
		t.Fatalf("state = %q, want COMPLETED", row.State)
	}
}

func TestTaskHandlerPatchGoldenCriteria(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)
	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(body))
		req.SetPathValue("id", taskID)
		rec := httptest.NewRecorder()
		h.Patch(rec, req)
		return rec
	}
	rec := patch(`{"success_criteria":["tests pass"],"golden_criteria":[{"criterion":"tests pass","check_command":"go test ./..."}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	task, err := store.GetTask(context.Background(), taskID)
	if err != nil || len(task.GoldenCriteria) != 1 || task.GoldenCriteria[0].CheckCommand != "go test ./..." {
		t.Fatalf("task = %+v, err = %v", task, err)
	}
	rec = patch(`{"golden_criteria":[{"criterion":"unknown","check_command":"true"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid criteria status = %d: %s", rec.Code, rec.Body.String())
	}
	running, err := store.MarkTaskRunning(context.Background(), task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	rec = patch(`{"golden_criteria":[]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("running task status = %d: %s", rec.Code, rec.Body.String())
	}
	got, err := store.GetTask(context.Background(), running.ID)
	if err != nil || len(got.GoldenCriteria) != 1 {
		t.Fatalf("running criteria changed: %+v, %v", got, err)
	}
}
