package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/testutil"
)

// T-050: every JSON handler caps its request body; an oversized but valid
// JSON document is rejected with 400 instead of being read into memory.
func TestJSONHandlersRejectOversizedBody(t *testing.T) {
	store := testutil.NewFakeStore()
	tasks := controllers.TaskHandler{Store: store}
	agents := controllers.AgentHandler{}
	prefs := controllers.PreferencesHandler{Store: store}
	projects := controllers.ProjectHandler{Store: store}
	chat := controllers.ChatHandler{}
	huge := strings.Repeat("a", 6<<20)
	cases := []struct {
		name string
		body string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"comment", `{"content":"` + huge + `"}`, tasks.AddComment},
		{"patch", `{"description":"` + huge + `"}`, tasks.Patch},
		{"agent create", `{"system_prompt":"` + huge + `"}`, agents.Create},
		{"preferences", `{"user_id":"u","text":"` + huge + `"}`, prefs.Save},
		{"materialize", `{"name":"` + huge + `"}`, projects.Materialize},
		{"preferences whitespace suffix", `{"user_id":"u","text":"hi"}` + strings.Repeat(" ", 2<<20), prefs.Save},
		{"chat", `{"messages":[{"role":"user","content":"` + huge + `"}]}`, chat.Complete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.SetPathValue("id", "t1")
			rec := httptest.NewRecorder()
			tc.call(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400; body=%.200s", rec.Code, rec.Body.String())
			}
		})
	}
}
