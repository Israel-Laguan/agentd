package controllers_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// B-065: sandbox.scrub_patterns hid secrets from tool output and logs but not
// from the events the API serves, which used a scrubber with no custom patterns.
func TestListEventsAppliesTheConfiguredScrubber(t *testing.T) {
	h, store := taskTestHandler()
	h.Scrubber = sandbox.NewScrubber([]string{`ACME-[0-9]{6}`})
	_, taskID := seedProjectTask(t, store)
	err := store.AppendEvent(context.Background(), models.Event{
		BaseEntity: models.BaseEntity{ID: "e1"}, ProjectID: store.Tasks()[0].ProjectID,
		TaskID: sql.NullString{String: taskID, Valid: true}, Type: models.EventTypeLog,
		Payload: "token ACME-123456 leaked",
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/events", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()

	h.ListEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ACME-123456") {
		t.Fatalf("custom-pattern secret reached the events response: %s", rec.Body.String())
	}
}
