package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/httpx"
)

func originWrite(t *testing.T, uiOrigin, origin string) (int, bool) {
	t.Helper()
	trusted := false
	h := guardMiddleware(guardConfig{uiOrigin: uiOrigin}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trusted = httpx.FromTrustedOrigin(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader("{}"))
	req.Host = "localhost:8765"
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, trusted
}

func TestGuardTrustsOnlyTheConfiguredUIOrigin(t *testing.T) {
	cases := []struct {
		name, uiOrigin, origin string
		want                   int
		trusted                bool
	}{
		{"configured origin", "http://board.example:3000", "http://board.example:3000", http.StatusNoContent, true},
		{"trailing slash in config", "http://localhost:3000/", "http://localhost:3000", http.StatusNoContent, true},
		{"default origin when another is configured", "http://board.example:3000", "http://localhost:3000", http.StatusForbidden, false},
		{"no UI origin configured", "", "http://localhost:3000", http.StatusForbidden, false},
		{"no origin header is allowed but not trusted", "http://localhost:3000", "", http.StatusNoContent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, trusted := originWrite(t, tc.uiOrigin, tc.origin)
			if code != tc.want || trusted != tc.trusted {
				t.Fatalf("status = %d trusted = %v, want %d %v", code, trusted, tc.want, tc.trusted)
			}
		})
	}
}

func TestCorsMiddleware_ConfiguredOrigin(t *testing.T) {
	h := corsMiddleware("http://board.example:3000", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	for origin, want := range map[string]string{
		"http://board.example:3000": "http://board.example:3000",
		"http://localhost:3000":     "",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s: Access-Control-Allow-Origin = %q, want %q", origin, got, want)
		}
	}
}
