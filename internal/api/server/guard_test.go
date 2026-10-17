package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func guardedOK(allowed ...string) http.Handler {
	return guardMiddleware(allowed, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

type guardCase struct {
	name        string
	method      string
	host        string
	origin      string
	contentType string
	body        string
	allowed     []string
	want        int
}

func runGuardCase(t *testing.T, tc guardCase) {
	t.Helper()
	var body *strings.Reader
	if tc.body != "" {
		body = strings.NewReader(tc.body)
	}
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(tc.method, "/api/v1/x", body)
	} else {
		req = httptest.NewRequest(tc.method, "/api/v1/x", nil)
	}
	if tc.host != "" {
		req.Host = tc.host
	}
	if tc.origin != "" {
		req.Header.Set("Origin", tc.origin)
	}
	if tc.contentType != "" {
		req.Header.Set("Content-Type", tc.contentType)
	}
	rec := httptest.NewRecorder()
	guardedOK(tc.allowed...).ServeHTTP(rec, req)
	if rec.Code != tc.want {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.want, rec.Body.String())
	}
}

func TestGuardRefusesNonJSONWrites(t *testing.T) {
	const body = `{"a":1}`
	cases := []guardCase{
		{name: "cross-site text/plain POST", method: http.MethodPost, host: "127.0.0.1:8765", origin: "https://evil.example", contentType: "text/plain", body: body, want: http.StatusForbidden},
		{name: "text/plain POST without origin", method: http.MethodPost, host: "localhost:8765", contentType: "text/plain", body: body, want: http.StatusUnsupportedMediaType},
		{name: "missing content type POST", method: http.MethodPost, host: "localhost:8765", body: body, want: http.StatusUnsupportedMediaType},
		{name: "form-encoded PATCH", method: http.MethodPatch, host: "localhost:8765", contentType: "application/x-www-form-urlencoded", body: "a=1", want: http.StatusUnsupportedMediaType},
		{name: "text/plain DELETE with body", method: http.MethodDelete, host: "localhost:8765", contentType: "text/plain", body: body, want: http.StatusUnsupportedMediaType},
		{name: "json POST", method: http.MethodPost, host: "localhost:8765", contentType: "application/json", body: body, want: http.StatusNoContent},
		{name: "json with charset", method: http.MethodPatch, host: "localhost:8765", contentType: "application/json; charset=utf-8", body: body, want: http.StatusNoContent},
		{name: "trusted UI origin json POST", method: http.MethodPost, host: "localhost:8765", origin: "http://localhost:3000", contentType: "application/json", body: body, want: http.StatusNoContent},
		{name: "bodiless POST", method: http.MethodPost, host: "localhost:8765", want: http.StatusNoContent},
		{name: "bodiless POST from foreign origin", method: http.MethodPost, host: "localhost:8765", origin: "https://evil.example", want: http.StatusForbidden},
		{name: "json POST from foreign origin", method: http.MethodPost, host: "localhost:8765", origin: "https://evil.example", contentType: "application/json", body: body, want: http.StatusForbidden},
		{name: "GET from foreign origin is not blocked here", method: http.MethodGet, host: "localhost:8765", origin: "https://evil.example", want: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runGuardCase(t, tc) })
	}
}

func TestGuardHostAllowlist(t *testing.T) {
	cases := []guardCase{
		{name: "localhost", method: http.MethodGet, host: "localhost:8765", want: http.StatusNoContent},
		{name: "ipv4 loopback", method: http.MethodGet, host: "127.0.0.1:8765", want: http.StatusNoContent},
		{name: "ipv6 loopback", method: http.MethodGet, host: "[::1]:8765", want: http.StatusNoContent},
		{name: "loopback without port", method: http.MethodGet, host: "localhost", want: http.StatusNoContent},
		{name: "rebound hostname", method: http.MethodGet, host: "evil.example:8765", want: http.StatusForbidden},
		{name: "rebound hostname on write", method: http.MethodPost, host: "evil.example", contentType: "application/json", body: "{}", want: http.StatusForbidden},
		{name: "lan address", method: http.MethodGet, host: "192.168.1.20:8765", want: http.StatusForbidden},
		{name: "configured host", method: http.MethodGet, host: "agentd.internal:8765", allowed: []string{"agentd.internal"}, want: http.StatusNoContent},
		{name: "configured host is case-insensitive", method: http.MethodGet, host: "Agentd.Internal", allowed: []string{"agentd.internal"}, want: http.StatusNoContent},
		{name: "configured host with port", method: http.MethodGet, host: "agentd:8765", allowed: []string{"agentd:8765"}, want: http.StatusNoContent},
		{name: "other port of configured host with port", method: http.MethodGet, host: "agentd:9", allowed: []string{"agentd:8765"}, want: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runGuardCase(t, tc) })
	}
}
