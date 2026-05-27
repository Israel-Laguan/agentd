package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/frontdesk"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func TestSystemGetDefaultExcludesHealing(t *testing.T) {
	store := testutil.NewFakeStore()
	// Create a user project with one normal task and one healing/handoff task.
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "user-proj",
		Tasks: []models.DraftTask{
			{Title: "normal-task", Description: "work"},
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	// Block with a HITL subtask to simulate healing handoff.
	_, _, err = store.BlockTaskWithSubtasks(context.Background(), tasks[0].ID, tasks[0].UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualReview + " AI providers unavailable",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	h := controllers.SystemHandler{System: sys}

	// Default: no include_healing param → healing tasks excluded.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Status struct {
				Summary struct {
					TasksByState map[string]int `json:"tasks_by_state"`
				} `json:"summary"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	byState := resp.Data.Status.Summary.TasksByState
	// The HITL subtask (READY, assigned HUMAN) should be excluded.
	total := 0
	for _, count := range byState {
		total += count
	}
	if total != 1 {
		t.Fatalf("expected 1 task (normal only), got %d: %v", total, byState)
	}
}

func TestSystemGetIncludeHealingTrue(t *testing.T) {
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "user-proj-2",
		Tasks: []models.DraftTask{
			{Title: "normal-task", Description: "work"},
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	_, _, err = store.BlockTaskWithSubtasks(context.Background(), tasks[0].ID, tasks[0].UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualReview + " self-healing failed",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	h := controllers.SystemHandler{System: sys}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status?include_healing=true", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Status struct {
				Summary struct {
					TasksByState map[string]int `json:"tasks_by_state"`
				} `json:"summary"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	byState := resp.Data.Status.Summary.TasksByState
	total := 0
	for _, count := range byState {
		total += count
	}
	// With include_healing=true, the HITL task should be counted.
	if total < 2 {
		t.Fatalf("expected at least 2 tasks (normal + healing), got %d: %v", total, byState)
	}
}

func TestSystemGetExcludesSystemProjectByDefault(t *testing.T) {
	store := testutil.NewFakeStore()
	// Create a user project.
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "user-proj-3",
		Tasks:       []models.DraftTask{{Title: "normal", Description: "w"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	// Create _system project with a task.
	sysProj, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	_, _, err = store.EnsureProjectTask(context.Background(), sysProj.ID, models.DraftTask{
		Title:    "System Offline alert",
		Assignee: models.TaskAssigneeHuman,
	})
	if err != nil {
		t.Fatalf("EnsureProjectTask: %v", err)
	}

	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	h := controllers.SystemHandler{System: sys}

	// Default: _system excluded.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Status struct {
				Summary struct {
					TotalProjects int `json:"total_projects"`
				} `json:"summary"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Status.Summary.TotalProjects != 1 {
		t.Fatalf("expected 1 project (excluding _system), got %d", resp.Data.Status.Summary.TotalProjects)
	}
}
