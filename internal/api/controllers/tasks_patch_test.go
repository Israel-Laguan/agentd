package controllers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

// patchFailStore wraps FakeKanbanStore but makes UpdateTaskPatch return a
// configurable error, simulating a failure after neither field is committed.
type patchFailStore struct {
	*testutil.FakeKanbanStore
	patchErr error
}

func (s *patchFailStore) UpdateTaskPatch(_ context.Context, _ string, _ time.Time, _ *models.TaskState, _ *string) (*models.Task, error) {
	return nil, s.patchErr
}

// TestTaskHandler_PatchStateAndDescription_PartialFailure verifies that when
// UpdateTaskPatch fails the API returns an error and neither field is mutated.
func TestTaskHandler_PatchStateAndDescription_PartialFailure(t *testing.T) {
	inner := testutil.NewFakeStore()
	_, taskID := seedProjectTask(t, inner)

	origTask, err := inner.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	origState := origTask.State
	origDesc := origTask.Description

	patchErr := errors.New("simulated store failure")
	wrapped := &patchFailStore{FakeKanbanStore: inner, patchErr: patchErr}
	svc := services.NewTaskService(wrapped, nil)
	h := controllers.TaskHandler{Store: inner, Tasks: svc}

	body := `{"state":"completed","description":"should not be set"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected error response, got 200 OK: %s", rec.Body.String())
	}

	afterTask, err := inner.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask after failed patch: %v", err)
	}
	if afterTask.State != origState {
		t.Fatalf("state mutated: got %q, want %q", afterTask.State, origState)
	}
	if afterTask.Description != origDesc {
		t.Fatalf("description mutated: got %q, want %q", afterTask.Description, origDesc)
	}
}
