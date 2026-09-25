package controllers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
)

type humanResolverStub struct {
	called     bool
	taskID     string
	expectedAt *time.Time
	result     string
	resolution *models.HumanHandoffResolution
	err        error
}

func (s *humanResolverStub) ResolveHumanHandoff(_ context.Context, taskID string, expectedAt *time.Time, result string) (*models.HumanHandoffResolution, error) {
	s.called = true
	s.taskID = taskID
	s.expectedAt = expectedAt
	s.result = result
	return s.resolution, s.err
}

func TestTaskEventsAndHumanResolutionController(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)
	ctx := context.Background()
	for i, payload := range []string{"first", "second", "api_key=abcdefghijklmnopqrstuvwxyz " + strings.Repeat("x", 17_000)} {
		if err := store.AppendEvent(ctx, models.Event{BaseEntity: models.BaseEntity{ID: fmt.Sprintf("event-%d", i)}, ProjectID: store.Tasks()[0].ProjectID, TaskID: sql.NullString{String: taskID, Valid: true}, Type: models.EventTypeLog, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/events?limit=2", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.ListEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events code = %d", rec.Code)
	}
	var events struct {
		Data []struct {
			Payload string `json:"payload"`
		} `json:"data"`
		Meta struct {
			Total     int  `json:"total"`
			HasMore   bool `json:"has_more"`
			Truncated bool `json:"truncated"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if events.Meta.Total != 3 || !events.Meta.HasMore || len(events.Data) != 2 || !strings.Contains(events.Data[1].Payload, "[REDACTED]") {
		t.Fatalf("events response = %+v", events)
	}

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/tasks/missing/events", http.StatusNotFound},
		{"/api/v1/tasks/" + taskID + "/events?limit=201", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		id := taskID
		if tc.want == http.StatusNotFound {
			id = "missing"
		}
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h.ListEvents(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s code = %d", tc.path, rec.Code)
		}
	}
}

func TestTaskHumanResolutionController(t *testing.T) {
	stamp := time.Now().UTC()
	resolver := &humanResolverStub{resolution: &models.HumanHandoffResolution{Task: &models.Task{BaseEntity: models.BaseEntity{ID: "child"}, State: models.TaskStateCompleted}, Parent: &models.Task{BaseEntity: models.BaseEntity{ID: "parent"}, State: models.TaskStateCompleted}, Result: "operator output"}}
	h := controllers.TaskHandler{HumanResolver: resolver, MaterializeToken: "secret"}
	body := fmt.Sprintf(`{"result":"operator output","expected_updated_at":%q}`, stamp.Format(time.RFC3339Nano))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/child/human-resolution", strings.NewReader(body))
	req.SetPathValue("id", "child")
	req.Header.Set("X-Agentd-Materialize-Token", "secret")
	rec := httptest.NewRecorder()
	h.ResolveHumanHandoff(rec, req)
	if rec.Code != http.StatusOK || !resolver.called || resolver.expectedAt == nil || !resolver.expectedAt.Equal(stamp) {
		t.Fatalf("resolution response = %d, call = %+v", rec.Code, resolver)
	}

	resolver = &humanResolverStub{}
	h = controllers.TaskHandler{HumanResolver: resolver, MaterializeToken: "secret"}
	for _, tc := range []struct {
		token string
		body  string
		want  int
	}{
		{"", `{"result":"ok"}`, http.StatusForbidden},
		{"secret", `{"result":" "}`, http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/child/human-resolution", strings.NewReader(tc.body))
		req.SetPathValue("id", "child")
		if tc.token != "" {
			req.Header.Set("X-Agentd-Materialize-Token", tc.token)
		}
		rec := httptest.NewRecorder()
		h.ResolveHumanHandoff(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("resolution code = %d, want %d", rec.Code, tc.want)
		}
	}
	if resolver.called {
		t.Fatal("resolver called for rejected request")
	}
}
