package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/testutil"
)

// postMaterializeAsUser is postMaterialize with the identity header the
// controller uses to scope memory recall to one user's saved preferences.
func postMaterializeAsUser(t *testing.T, h controllers.ProjectHandler, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-Agentd-User", userID)
	}
	rec := httptest.NewRecorder()
	h.Materialize(rec, req)
	return rec
}

func materializeUserBody(t *testing.T, projectName, userID string) string {
	t.Helper()
	body, err := json.Marshal(struct {
		ProjectName string `json:"project_name"`
		UserID      string `json:"user_id"`
		Tasks       []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"tasks"`
	}{
		ProjectName: projectName,
		UserID:      userID,
		Tasks: []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}{{Title: "Build", Description: "work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func materializedProjectUserID(t *testing.T, store *testutil.FakeKanbanStore, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", rec.Code, rec.Body.String())
	}
	projectID, _ := parseMaterializeResponse(t, rec)
	project, err := store.GetProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("GetProject(%s): %v", projectID, err)
	}
	return project.UserID
}

// TestMaterializeHeaderOverridesBodyUserID pins the trust boundary: the
// requesting identity comes from the X-Agentd-User header, never from the
// caller-controlled body. A body user_id must not be able to choose whose
// saved preferences are recalled at execution time.
func TestMaterializeHeaderOverridesBodyUserID(t *testing.T) {
	h, store := projectTestHandler(t)

	rec := postMaterializeAsUser(t, h, materializeUserBody(t, "header-wins", "mallory"), "alice")
	if got := materializedProjectUserID(t, store, rec); got != "alice" {
		t.Fatalf("project.user_id = %q, want %q (header must win over body)", got, "alice")
	}
}

// TestMaterializeBlankHeaderClearsBodyUserID covers the other half: with no
// header the project must have no user, even when the body claims one. A blank
// header previously left the body value in place.
func TestMaterializeBlankHeaderClearsBodyUserID(t *testing.T) {
	h, store := projectTestHandler(t)

	rec := postMaterializeAsUser(t, h, materializeUserBody(t, "no-header", "mallory"), "")
	if got := materializedProjectUserID(t, store, rec); got != "" {
		t.Fatalf("project.user_id = %q, want \"\" (body-supplied identity must not survive an absent header)", got)
	}
}

// TestMaterializeHeaderIsTrimmed keeps a padded header from being stamped
// verbatim, which would scope recall to an identity that was never saved.
func TestMaterializeHeaderIsTrimmed(t *testing.T) {
	h, store := projectTestHandler(t)

	rec := postMaterializeAsUser(t, h, materializeUserBody(t, "padded-header", ""), "  alice  ")
	if got := materializedProjectUserID(t, store, rec); got != "alice" {
		t.Fatalf("project.user_id = %q, want %q (header should be trimmed)", got, "alice")
	}
}
