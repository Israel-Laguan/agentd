package controllers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

// The web board pages a project's tasks and must learn when a page is not the
// last one (B-071), so the list carries meta.has_more like the events list.
func TestTaskHandler_ListByProjectReportsHasMore(t *testing.T) {
	store := testutil.NewFakeStore()
	h := controllers.TaskHandler{Store: store, Tasks: services.NewTaskService(store, store)}
	draft := models.DraftPlan{ProjectName: "paged"}
	for i := range 3 {
		draft.Tasks = append(draft.Tasks, models.DraftTask{Title: fmt.Sprintf("t%d", i), Description: "work"})
	}
	proj, _, err := store.MaterializePlan(context.Background(), draft)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	for _, tc := range []struct {
		query   string
		hasMore bool
	}{
		{"limit=2&offset=0", true},
		{"limit=2&offset=2", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+proj.ID+"/tasks?"+tc.query, nil)
		req.SetPathValue("id", proj.ID)
		rec := httptest.NewRecorder()
		h.ListByProject(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code = %d body = %s", tc.query, rec.Code, rec.Body.String())
		}
		var resp struct {
			Meta struct {
				Total   int  `json:"total"`
				HasMore bool `json:"has_more"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Meta.Total != 3 || resp.Meta.HasMore != tc.hasMore {
			t.Fatalf("%s: meta = %+v, want total 3 has_more %v", tc.query, resp.Meta, tc.hasMore)
		}
	}
}
