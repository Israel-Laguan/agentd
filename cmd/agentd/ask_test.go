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
