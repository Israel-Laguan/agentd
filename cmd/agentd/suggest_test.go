package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/kanban"
	"agentd/internal/models"
)

func TestSuggestTaskPrintsCommandAndLogsEvent(t *testing.T) {
	server := newOpenAITestServer(t)
	defer server.Close()
	home, taskID := seedSuggestTask(t, server.URL)
	var output bytes.Buffer
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "suggest-task", taskID})
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("suggest-task error = %v", err)
	}
	if !strings.Contains(output.String(), "&& echo hi") {
		t.Fatalf("output = %s", output.String())
	}
	assertSuggestionEvent(t, filepath.Join(home, "global.db"))
}

func newOpenAITestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]any{
			"model": "gpt-test",
			"choices": []map[string]any{{
				"message": gateway.PromptMessage{Role: "assistant", Content: `{"command":"echo hi"}`},
			}},
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
}

func seedSuggestTask(t *testing.T, openAIURL string) (string, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".agentd")
	t.Setenv("AGENTD_GATEWAY_OPENAI_BASE_URL", openAIURL)
	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")
	t.Setenv("AGENTD_GATEWAY_ORDER", "openai")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	store, err := kanban.OpenStore(filepath.Join(home, "global.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := seedDefaultAgent(context.Background(), store); err != nil {
		t.Fatalf("seedDefaultAgent() error = %v", err)
	}
	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "suggest",
		Description: "suggest command",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A", Description: "Say hi"}},
	})
	if err != nil || project == nil {
		t.Fatalf("MaterializePlan() project=%v error=%v", project, err)
	}
	return home, tasks[0].ID
}

func assertSuggestionEvent(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE type = 'SUGGESTION'`).Scan(&count); err != nil {
		t.Fatalf("count suggestion events: %v", err)
	}
	if count != 1 {
		t.Fatalf("suggestion events = %d, want 1", count)
	}
}
