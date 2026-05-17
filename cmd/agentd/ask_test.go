package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestAskApprovesDraftedPlan(t *testing.T) {
	materialized := false
	server := askTestServer(t, &materialized)
	output := runAskTest(t, server.URL, "Y\n")
	if !materialized {
		t.Fatal("materialize endpoint was not called")
	}
	if !strings.Contains(output, "Do you approve this plan? [Y/n]") || !strings.Contains(output, "project started") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestAskRejectsDraftedPlan(t *testing.T) {
	materialized := false
	server := askTestServer(t, &materialized)
	output := runAskTest(t, server.URL, "N\n")
	if materialized {
		t.Fatal("materialize endpoint was called")
	}
	if !strings.Contains(output, "plan rejected") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func runAskTest(t *testing.T, apiURL, input string) string {
	t.Helper()
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", filepath.Join(t.TempDir(), ".agentd"), "ask", "Build a node app", "--api-url", apiURL})
	cmd.SetIn(strings.NewReader(input))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ask error = %v", err)
	}
	return output.String()
}

func askTestServer(t *testing.T, materialized *bool) *httptest.Server {
	t.Helper()
	plan := models.DraftPlan{ProjectName: "node app", Tasks: []models.DraftTask{{TempID: "a", Title: "Create app"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			writeAskCompletion(t, w, plan)
		case "/api/v1/projects/materialize":
			*materialized = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func decodeDraftSuccessBody(t *testing.T) string {
	t.Helper()
	plan := models.DraftPlan{ProjectName: "app", Tasks: []models.DraftTask{{TempID: "1", Title: "t"}}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(struct {
		Choices []struct {
			Message gateway.PromptMessage `json:"message"`
		} `json:"choices"`
	}{Choices: []struct {
		Message gateway.PromptMessage `json:"message"`
	}{{Message: gateway.PromptMessage{Role: "assistant", Content: string(planJSON)}}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestDecodeDraft(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "success", status: http.StatusOK, body: decodeDraftSuccessBody(t)},
		{name: "not found", status: http.StatusNotFound, body: `{"choices":[]}`, wantErr: "draft request failed"},
		{name: "bad json", status: http.StatusOK, body: `{invalid`},
		{name: "clarification kind", status: http.StatusOK, body: `{"choices":[{"message":{"role":"assistant","content":"{\"kind\":\"feasibility_clarification\"}"}}]}`, wantErr: "feasibility_clarification"},
		{name: "empty choices", status: http.StatusOK, body: `{"choices":[]}`, wantErr: "draft request failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runDecodeDraftCase(t, tt.name, tt.status, tt.body, tt.wantErr)
		})
	}
}

func runDecodeDraftCase(t *testing.T, name string, status int, body, wantErr string) {
	t.Helper()
	rec := httptest.NewRecorder()
	rec.Code = status
	rec.Body.WriteString(body)
	got, err := decodeDraft(rec.Result())
	if wantErr == "" && name == "success" {
		if err != nil {
			t.Fatalf("decodeDraft() error = %v", err)
		}
		if got.ProjectName != "app" {
			t.Fatalf("plan = %+v", got)
		}
		return
	}
	if wantErr == "" && name == "bad json" {
		if err == nil {
			t.Fatal("decodeDraft() error = nil, want decode error")
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("decodeDraft() error = %v, want containing %q", err, wantErr)
	}
}

func TestApproved(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{name: "yes default", input: "\n", want: true},
		{name: "y", input: "y\n", want: true},
		{name: "yes", input: "yes\n", want: true},
		{name: "no", input: "n\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(tt.input))
			got, err := approved(cmd)
			if tt.wantErr {
				if err == nil {
					t.Fatal("approved() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("approved() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("approved() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaterializePlan_SendsToken(t *testing.T) {
	var gotToken, gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Agentd-Materialize-Token")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	cfg := config.Config{API: config.APIConfig{MaterializeToken: "secret-token"}}
	plan := models.DraftPlan{ProjectName: "p"}
	if err := materializePlan(cmd, server.Client(), server.URL, cfg, plan); err != nil {
		t.Fatalf("materializePlan() error = %v", err)
	}
	if gotToken != "secret-token" {
		t.Fatalf("token = %q, want secret-token", gotToken)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/api/v1/projects/materialize" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/v1/projects/materialize")
	}
}

func writeAskCompletion(t *testing.T, w http.ResponseWriter, plan models.DraftPlan) {
	t.Helper()
	content, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	resp := struct {
		Choices []struct {
			Message gateway.PromptMessage `json:"message"`
		} `json:"choices"`
	}{Choices: []struct {
		Message gateway.PromptMessage `json:"message"`
	}{{Message: gateway.PromptMessage{Role: "assistant", Content: string(content)}}}}
	_ = json.NewEncoder(w).Encode(resp)
}
