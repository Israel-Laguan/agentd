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

// stubResetter is a minimal services.BreakerResetter for tests.
type stubResetter struct{ called bool }

func (r *stubResetter) Reset() { r.called = true }

func TestSystemResetNoService(t *testing.T) {
	h := controllers.SystemHandler{System: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/breaker/reset", nil)
	rec := httptest.NewRecorder()
	h.Reset(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

func TestSystemResetNoResetter(t *testing.T) {
	store := testutil.NewFakeStore()
	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	// Resetter and ProviderBreakers are both nil.
	h := controllers.SystemHandler{System: sys}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/breaker/reset", nil)
	rec := httptest.NewRecorder()
	h.Reset(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503 when no resetter configured", rec.Code)
	}
}

func TestSystemResetSuccess(t *testing.T) {
	store := testutil.NewFakeStore()
	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	resetter := &stubResetter{}
	sys.Resetter = resetter
	h := controllers.SystemHandler{System: sys}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/breaker/reset", nil)
	rec := httptest.NewRecorder()
	h.Reset(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !resetter.called {
		t.Fatal("Reset() was not called on the resetter")
	}
}

// stubProviderBreakers implements services.ProviderBreakersProbe for tests.
type stubProviderBreakers struct {
	resetProvider string
	resetAllCalled bool
}

func (s *stubProviderBreakers) Snapshot() map[string]services.ProviderBreakerEntry {
	return nil
}
func (s *stubProviderBreakers) Reset(provider string) { s.resetProvider = provider }
func (s *stubProviderBreakers) ResetAll()             { s.resetAllCalled = true }

func TestSystemResetProviderSuccess(t *testing.T) {
	store := testutil.NewFakeStore()
	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	pb := &stubProviderBreakers{}
	sys.ProviderBreakers = pb
	h := controllers.SystemHandler{System: sys}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/breaker/reset?provider=gemini", nil)
	rec := httptest.NewRecorder()
	h.Reset(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if pb.resetProvider != "gemini" {
		t.Fatalf("Reset called with %q, want %q", pb.resetProvider, "gemini")
	}
}

func TestSystemResetProviderNoBreakers(t *testing.T) {
	store := testutil.NewFakeStore()
	sum := frontdesk.NewStatusSummarizer(store)
	sys := services.NewSystemService(sum, nil)
	// ProviderBreakers is nil — per-provider reset must fail.
	h := controllers.SystemHandler{System: sys}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/breaker/reset?provider=gemini", nil)
	rec := httptest.NewRecorder()
	h.Reset(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503 when no provider breakers configured", rec.Code)
	}
}
