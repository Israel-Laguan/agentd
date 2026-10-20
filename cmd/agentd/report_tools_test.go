package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const toolUseBody = `{"status":"success","data":{"tools":[
 {"tool":"read","calls":5,"failed":0,"result_bytes":4050},{"tool":"bash","calls":2,"failed":1,"result_bytes":10}],
 "reads":5,"read_bytes":4050,"reread_bytes":2000,
 "repeated_reads":[{"task_id":"t1","path":"a.go","reads":3,"bytes":3000,"reread_bytes":2000}]}}`

func TestReportToolsPrintsToolCountsAndRepeatedReads(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/reports/tool-use" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(toolUseBody))
	}))
	t.Cleanup(srv.Close)
	out := runCLI(t, t.TempDir(), "report", "tools", "--api-url", srv.URL, "--task", "t1", "--from", "2026-10-18")
	for _, want := range []string{"TOOL", "FAILED", "bash", "4050", "re-read: 2000 of 4050 bytes read", "a.go", "t1", "3", "2000"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(query, "task_id=t1") || !strings.Contains(query, "from=2026-10-18") {
		t.Errorf("query = %q, want the filters forwarded", query)
	}
}
