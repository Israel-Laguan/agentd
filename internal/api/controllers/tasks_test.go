package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func taskTestHandler() (controllers.TaskHandler, *testutil.FakeKanbanStore) {
	store := testutil.NewFakeStore()
	svc := services.NewTaskService(store, nil)
	return controllers.TaskHandler{Store: store, Tasks: svc}, store
}

func seedProjectTask(t *testing.T, store *testutil.FakeKanbanStore) (projectID, taskID string) {
	t.Helper()
	ctx := context.Background()
	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "test-project",
		Tasks:       []models.DraftTask{{Title: "Task One", Description: "do work"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one task")
	}
	return proj.ID, tasks[0].ID
}

func TestTaskHandler_Patch(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	body := `{"state":"completed"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Patch code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			ID    string `json:"id"`
			State string `json:"state"`
			Agent *struct {
				ID string `json:"id"`
			} `json:"agent"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ID != taskID {
		t.Fatalf("task id = %q, want %q", resp.Data.ID, taskID)
	}
	if resp.Data.State != string(models.TaskStateCompleted) {
		t.Fatalf("state = %q, want COMPLETED", resp.Data.State)
	}
	if resp.Data.Agent == nil || resp.Data.Agent.ID != "default" {
		t.Fatal("expected embedded default agent profile")
	}
}

func TestTaskHandler_PatchValidation(t *testing.T) {
	h, _ := taskTestHandler()

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/t1", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		h.Patch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("empty state and no description", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/t1", strings.NewReader(`{"state":"  "}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		h.Patch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/t1", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		h.Patch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("nil task service", func(t *testing.T) {
		bare := controllers.TaskHandler{Store: testutil.NewFakeStore()}
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/t1", strings.NewReader(`{"state":"ready"}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		bare.Patch(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("nil task service for description", func(t *testing.T) {
		bare := controllers.TaskHandler{Store: testutil.NewFakeStore()}
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/t1", strings.NewReader(`{"description":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		bare.Patch(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d", rec.Code)
		}
	})
}

func TestTaskHandler_PatchDescription(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	body := `{"description":"updated work"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Patch code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Description string `json:"Description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Description != "updated work" {
		t.Fatalf("description = %q, want updated work", resp.Data.Description)
	}
}

func TestTaskHandler_PatchStateAndDescription(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	body := `{"state":"completed","description":"done"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Patch code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			State       string `json:"state"`
			Description string `json:"Description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.State != string(models.TaskStateCompleted) {
		t.Fatalf("state = %q, want COMPLETED", resp.Data.State)
	}
	if resp.Data.Description != "done" {
		t.Fatalf("description = %q, want done", resp.Data.Description)
	}
}

func TestTaskHandler_ListByProject(t *testing.T) {
	h, store := taskTestHandler()
	projectID, _ := seedProjectTask(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/tasks?state=READY&assignee=SYSTEM&limit=10&offset=0", nil)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	h.ListByProject(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListByProject code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			ProjectID string `json:"ProjectID"`
		} `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected at least one task")
	}
	if resp.Data[0].ProjectID != projectID {
		t.Fatalf("ProjectID = %q, want %q", resp.Data[0].ProjectID, projectID)
	}
}

func TestTaskHandler_ListByProjectValidation(t *testing.T) {
	h, store := taskTestHandler()
	projectID, _ := seedProjectTask(t, store)

	t.Run("invalid query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/tasks?state=BOGUS&limit=abc", nil)
		req.SetPathValue("id", projectID)
		rec := httptest.NewRecorder()
		h.ListByProject(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("nil task service", func(t *testing.T) {
		bare := controllers.TaskHandler{Store: store}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/tasks", nil)
		req.SetPathValue("id", projectID)
		rec := httptest.NewRecorder()
		bare.ListByProject(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("missing project", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/missing/tasks", nil)
		req.SetPathValue("id", "missing")
		rec := httptest.NewRecorder()
		h.ListByProject(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
		}
	})
}

func TestTaskHandler_Assign(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{ID: "other-agent", Name: "Other"}); err != nil {
		t.Fatalf("UpsertAgentProfile: %v", err)
	}

	body := `{"agent_id":"other-agent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/assign", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Assign(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Assign code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			AgentID string `json:"AgentID"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.AgentID != "other-agent" {
		t.Fatalf("AgentID = %q, want other-agent", resp.Data.AgentID)
	}
}

func TestTaskHandler_AssignValidation(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	t.Run("empty agent_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/assign", strings.NewReader(`{"agent_id":"  "}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", taskID)
		rec := httptest.NewRecorder()
		h.Assign(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("unknown agent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/assign", strings.NewReader(`{"agent_id":"missing-agent"}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", taskID)
		rec := httptest.NewRecorder()
		h.Assign(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
		}
	})
}

func TestTaskHandler_Split(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	body := `{"subtasks":[{"title":"Sub A","description":"a"},{"title":"Sub B","description":"b"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/split", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Split(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Split code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Parent struct {
				State string `json:"state"`
			} `json:"parent"`
			Children []struct {
				Title string `json:"title"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Parent.State != string(models.TaskStateBlocked) {
		t.Fatalf("parent state = %q, want BLOCKED", resp.Data.Parent.State)
	}
	if len(resp.Data.Children) != 2 {
		t.Fatalf("children len = %d, want 2", len(resp.Data.Children))
	}
}

func TestTaskHandler_SplitValidation(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	t.Run("empty subtasks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/split", strings.NewReader(`{"subtasks":[]}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", taskID)
		rec := httptest.NewRecorder()
		h.Split(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d", rec.Code)
		}
	})
}

func TestTaskHandler_Retry(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)
	ctx := context.Background()
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, taskID, task.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/retry", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Retry(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Retry code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			State string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.State != string(models.TaskStateReady) {
		t.Fatalf("state = %q, want READY", resp.Data.State)
	}
}

func TestTaskHandler_RetryInvalidState(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/retry", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Retry(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("Retry from READY code = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestTaskHandler_ListComments(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	const body = "hello from test"
	if err := store.AddComment(context.Background(), models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorUser,
		Body:   body,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/comments", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.ListComments(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListComments code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		Data   []struct {
			TaskID  string `json:"TaskID"`
			Author  string `json:"Author"`
			Body    string `json:"Body"`
			Content string `json:"Content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "success" {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(comments) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].TaskID != taskID {
		t.Fatalf("TaskID = %q, want %q", resp.Data[0].TaskID, taskID)
	}
	if resp.Data[0].Author != string(models.CommentAuthorUser) {
		t.Fatalf("Author = %q, want %q", resp.Data[0].Author, models.CommentAuthorUser)
	}
	if resp.Data[0].Body != body {
		t.Fatalf("Body = %q, want %q", resp.Data[0].Body, body)
	}
	if resp.Data[0].Content != body {
		t.Fatalf("Content = %q, want %q", resp.Data[0].Content, body)
	}
}

func TestTaskHandler_ListCommentsEmpty(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/comments", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.ListComments(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListComments code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp.Data) != "[]" {
		t.Fatalf("data = %s, want []", resp.Data)
	}
}

func TestTaskHandler_ListCommentsNilStore(t *testing.T) {
	h := controllers.TaskHandler{Store: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/t1/comments", nil)
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	h.ListComments(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListComments code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp.Data) != "[]" {
		t.Fatalf("data = %s, want []", resp.Data)
	}
}
