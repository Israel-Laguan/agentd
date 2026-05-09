package controllers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/testutil"
)

func TestPreferencesSaveInvalidJSON(t *testing.T) {
	h := controllers.PreferencesHandler{Store: testutil.NewFakeStore()}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/preferences", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Save(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestPreferencesSaveValidation(t *testing.T) {
	h := controllers.PreferencesHandler{Store: testutil.NewFakeStore()}
	body := `{"user_id":"  ","text":"  "}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Save(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestPreferencesSaveSuccess(t *testing.T) {
	h := controllers.PreferencesHandler{Store: testutil.NewFakeStore()}
	body := `{"user_id":"alice","text":"prefer dark mode"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Save(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
}
