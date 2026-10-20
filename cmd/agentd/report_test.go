package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const usageBody = `{"status":"success","data":{"group_by":["purpose","model"],"rows":[
 {"group":{"purpose":"exec.turn","model":"m1"},"calls":3,"tokens":1200,"cached_tokens":100,"avg_latency_ms":812.5,"max_latency_ms":2000,"repair_rate":0.5,"cascade_rate":0.25},
 {"group":{"purpose":"intake.classify_intent","model":"m2"},"calls":1,"tokens":40,"avg_latency_ms":90,"max_latency_ms":90}],
 "totals":{"calls":4,"tokens":1240,"cached_tokens":100,"avg_latency_ms":600,"max_latency_ms":2000,"repair_rate":0.2,"cascade_rate":0.1},
 "dropped_records":3}}`

func usageServer(t *testing.T, status int, body string, seen *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/reports/ai-usage" {
			http.NotFound(w, r)
			return
		}
		*seen = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReportAIPrintsTheTableAndForwardsFilters(t *testing.T) {
	var query string
	srv := usageServer(t, http.StatusOK, usageBody, &query)
	out := runCLI(t, t.TempDir(), "report", "ai", "--api-url", srv.URL,
		"--group-by", "purpose,model", "--from", "2026-10-18", "--task", "t1", "--request", "req-1")
	for _, want := range []string{"PURPOSE", "MODEL", "CALLS", "TOKENS", "REPAIR%", "CASCADE%",
		"exec.turn", "m1", "1200", "50.0", "25.0", "TOTAL", "1240", "dropped records: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	for _, want := range []string{"group_by=purpose%2Cmodel", "from=2026-10-18", "task_id=t1", "request=req-1"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}
}

func TestReportAIJSONPrintsTheEnvelopeData(t *testing.T) {
	var query string
	srv := usageServer(t, http.StatusOK, usageBody, &query)
	out := runCLI(t, t.TempDir(), "report", "ai", "--api-url", srv.URL, "--json")
	if !strings.Contains(out, `"dropped_records": 3`) || strings.Contains(out, "PURPOSE") {
		t.Fatalf("--json output = %s", out)
	}
}

func TestReportAIShowsTheDaemonErrorMessage(t *testing.T) {
	var query string
	srv := usageServer(t, http.StatusBadRequest,
		`{"status":"error","error":{"code":"VALIDATION_FAILED","message":"unknown group_by \"x\""}}`, &query)
	cmd := newRootCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--home", t.TempDir(), "report", "ai", "--api-url", srv.URL, "--group-by", "x"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") || !strings.Contains(err.Error(), "unknown group_by") {
		t.Fatalf("err = %v, want the daemon's code and message", err)
	}
}

func TestReportAIWithoutADaemon(t *testing.T) {
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", t.TempDir(), "report", "ai", "--api-url", "http://127.0.0.1:1"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "daemon is not running") {
		t.Fatalf("err = %v, want the not-running hint", err)
	}
}
