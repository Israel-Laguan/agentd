package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/frontdesk"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func TestSystemGetServiceMissing(t *testing.T) {
	h := controllers.SystemHandler{System: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestSystemGetSuccess(t *testing.T) {
	store := testutil.NewFakeStore()
	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	h := controllers.SystemHandler{System: sys}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
}
