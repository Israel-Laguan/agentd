package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/models"
)

func newTestHandler(store *apiStore) http.Handler {
	return api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newAPIGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
}

func TestListTasksByProjectReturnsEnvelope(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodGet, "/api/v1/projects/project/tasks", "")
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	if body["status"] != "success" {
		t.Fatalf("status = %v, want success", body["status"])
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta missing or wrong type: %v", body["meta"])
	}
	if _, ok := meta["page"]; !ok {
		t.Fatalf("meta.page missing: %v", meta)
	}
}

func TestListTasksByProjectUnknownProjectReturnsNotFound(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodGet, "/api/v1/projects/missing/tasks", "")
	assertStatus(t, resp, http.StatusNotFound)
	assertNestedField(t, resp, "error", "code", "NOT_FOUND")
}

func TestListTasksByProjectRejectsBadStateFilter(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodGet, "/api/v1/projects/project/tasks?state=BOGUS", "")
	assertStatus(t, resp, http.StatusBadRequest)
	assertNestedField(t, resp, "error", "code", "VALIDATION_FAILED")
	body := decodeBody(t, resp)
	errObj := body["error"].(map[string]any)
	details, ok := errObj["details"].([]any)
	if !ok || len(details) == 0 {
		t.Fatalf("expected details list with at least one entry, got %v", errObj)
	}
}

func TestPatchTaskUpdatesState(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodPatch, "/api/v1/tasks/123", `{"state":"COMPLETED"}`)
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	if body["status"] != "success" {
		t.Fatalf("status = %v, want success", body["status"])
	}
}

func TestPatchTaskRejectsUnknownState(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodPatch, "/api/v1/tasks/123", `{"state":"NONSENSE"}`)
	assertStatus(t, resp, http.StatusConflict)
	assertNestedField(t, resp, "error", "code", "STATE_CONFLICT")
}

func TestPatchTaskMissingTaskReturnsNotFound(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodPatch, "/api/v1/tasks/missing", `{"state":"COMPLETED"}`)
	assertStatus(t, resp, http.StatusNotFound)
	assertNestedField(t, resp, "error", "code", "NOT_FOUND")
}

func TestSystemStatusReturnsSnapshot(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodGet, "/api/v1/system/status", "")
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	if body["status"] != "success" {
		t.Fatalf("status = %v, want success", body["status"])
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing: %+v", body)
	}
	if _, ok := data["status"]; !ok {
		t.Fatalf("system status payload missing status report: %+v", data)
	}
	if _, ok := data["memory"]; !ok {
		t.Fatalf("system status payload missing memory section: %+v", data)
	}
}

func TestUnifiedErrorEnvelopeIncludesDetailsForValidation(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)

	resp := request(handler, http.MethodPost, "/api/v1/tasks/123/comments", `{"content":"   "}`)
	assertStatus(t, resp, http.StatusBadRequest)
	body := decodeBody(t, resp)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "VALIDATION_FAILED" {
		t.Fatalf("error.code = %v, want VALIDATION_FAILED", errObj["code"])
	}
	if _, ok := errObj["details"]; !ok {
		t.Fatalf("error.details missing: %+v", errObj)
	}
}

// Smoke check that the comment route still pauses the task into IN_CONSIDERATION
// when the new TaskService delegates to the store fallback path.
func TestCommentStillPausesViaService(t *testing.T) {
	store := newAPITestStore()
	handler := newTestHandler(store)
	resp := request(handler, http.MethodPost, "/api/v1/tasks/123/comments", `{"content":"please pause"}`)
	assertStatus(t, resp, http.StatusCreated)
	task, err := store.GetTask(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != models.TaskStateInConsideration {
		t.Fatalf("state = %v, want IN_CONSIDERATION", task.State)
	}
	// Ensure response body is valid envelope JSON.
	if !json.Valid(resp.Body.Bytes()) {
		t.Fatalf("response not valid JSON: %s", resp.Body.String())
	}
}
