package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/api/controllers"
	"agentd/internal/frontdesk"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not the envelope: %v: %q", err, rec.Body.String())
	}
	return env.Error.Code
}

// transitionRefusedStore makes every state write fail as a forbidden transition.
type transitionRefusedStore struct{ *testutil.FakeKanbanStore }

func (s transitionRefusedStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, _ models.TaskState) (*models.Task, error) {
	return nil, models.ErrInvalidStateTransition
}

// T-054: an unknown state is a validation error (400); a transition the rules
// forbid is a state conflict (409).
func TestPatchStateStatusSplit(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		refuse   bool
		wantCode int
		wantName string
	}{
		{"unknown state", "bogus", false, http.StatusBadRequest, "VALIDATION_FAILED"},
		{"forbidden transition", "completed", true, http.StatusConflict, "STATE_CONFLICT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := testutil.NewFakeStore()
			_, taskID := seedProjectTask(t, inner)
			svc := services.NewTaskService(inner, nil)
			if tc.refuse {
				svc = services.NewTaskService(transitionRefusedStore{inner}, nil)
			}
			h := controllers.TaskHandler{Store: inner, Tasks: svc}
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(`{"state":"`+tc.state+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", taskID)
			rec := httptest.NewRecorder()
			h.Patch(rec, req)
			if rec.Code != tc.wantCode || errorCode(t, rec) != tc.wantName {
				t.Fatalf("got %d %s, want %d %s: %s", rec.Code, errorCode(t, rec), tc.wantCode, tc.wantName, rec.Body.String())
			}
		})
	}
}

// T-054: resetting a breaker for a provider that has none answers 404, not
// {"reset": true}.
func TestSystemResetUnknownProviderIs404(t *testing.T) {
	sys := services.NewSystemService(frontdesk.NewStatusSummarizer(testutil.NewFakeStore()), nil)
	sys.ProviderBreakers = &stubProviderBreakers{known: map[string]bool{"gemini": true}}
	h := controllers.SystemHandler{System: sys}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/breaker/reset?provider=typo", nil)
	rec := httptest.NewRecorder()
	h.Reset(rec, req)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "NOT_FOUND" {
		t.Fatalf("got %d %s, want 404 NOT_FOUND: %s", rec.Code, errorCode(t, rec), rec.Body.String())
	}
}
