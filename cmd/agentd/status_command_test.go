package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusCommand_PrintsCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mockStatusResponse(map[string]int{
			"ready": 1, "queued": 1, "running": 1, "completed": 1, "failed": 1,
		}))
	}))
	defer srv.Close()

	home := t.TempDir()
	output := runCLI(t, home, "status", "--api-url", srv.URL)
	for _, expect := range []string{
		"STATE             COUNT",
		"READY",
		"queue_length",
		"active_threads",
	} {
		if !strings.Contains(output, expect) {
			t.Errorf("output missing %q\nfull output:\n%s", expect, output)
		}
	}
}
