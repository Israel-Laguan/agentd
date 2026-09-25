package controllers_test

import (
	"net/http"
	"testing"

	"agentd/internal/models"
)

func TestProjectHandler_MaterializeStartEmptyWorkspace(t *testing.T) {
	h, _ := projectServiceTestHandler(t)
	body := `{"project_name":"empty-api","start_empty_workspace":true,"tasks":[{"temp_id":"root","title":"Root"},{"temp_id":"child","title":"Child","depends_on":["root"]}]}`
	rec := postMaterialize(t, h, body, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", rec.Code, rec.Body.String())
	}
	_, states := parseMaterializeResponse(t, rec)
	if len(states) != 2 || states[0] != string(models.TaskStateReady) || states[1] != string(models.TaskStatePending) {
		t.Fatalf("states = %v, want READY and PENDING", states)
	}
}
