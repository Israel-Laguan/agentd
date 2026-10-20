package main

import (
	"net/http"
	"strings"
	"testing"
)

const mixBody = `{"status":"success","data":{"group_by":["purpose"],"rows":[
 {"group":{"purpose":"exec.turn"},"calls":4,"input_chars":1000,
  "mix_chars":{"system":250,"memories":50,"task":100,"parent_outcome":0,"comments":0,"file_context":300,"history":100,"tool_results":200},
  "truncated_calls":3,"cut_by":{"working_zone":0,"strategy_budget":0,"agentic_head":3,"router":0}}],
 "totals":{"calls":4,"input_chars":1000,"mix_chars":{"system":250},"truncated_calls":3,"cut_by":{"agentic_head":3}},
 "dropped_records":0}}`

func TestReportAIMixPrintsSectionSharesAndCutLayers(t *testing.T) {
	var query string
	srv := usageServer(t, http.StatusOK, mixBody, &query)
	out := runCLI(t, t.TempDir(), "report", "ai", "--api-url", srv.URL, "--mix")
	for _, want := range []string{"SYSTEM%", "MEMORIES%", "FILE%", "TOOL%", "CUT%", "HEAD", "exec.turn", "25.0", "30.0", "75.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "REPAIR%") {
		t.Errorf("--mix must replace the token columns\n%s", out)
	}
}
