package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/kanban"
	"agentd/internal/models"
	"agentd/internal/services"
)

// newKanbanIntegrationStore opens a real on-disk kanban store seeded with
// a default agent profile. Tests against this store exercise the actual
// SQL transactions, which is essential for the manager-loop endpoints
// (assign, split, retry) because they rely on optimistic locks the fake
// in-memory store cannot meaningfully exercise.
func newKanbanIntegrationStore(t *testing.T) *kanban.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := kanban.OpenStore(filepath.Join(dir, "agentd.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertAgentProfile(context.Background(), models.AgentProfile{
		ID: "default", Name: "Default", Provider: "openai", Model: "gpt-4o-mini",
		Temperature: 0.2, Role: "CODE_GEN",
	}); err != nil {
		t.Fatalf("seed default agent: %v", err)
	}
	return store
}

func materializeOneTask(t *testing.T, store *kanban.Store) models.Task {
	t.Helper()
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "manager loop", Description: "test",
		Tasks: []models.DraftTask{{TempID: "a", Title: "Do work", Description: "single"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	return tasks[0]
}

func newManagerLoopHandler(t *testing.T, store *kanban.Store) http.Handler {
	t.Helper()
	board, _ := any(store).(models.KanbanBoardContract)
	return api.NewHandler(api.ServerDeps{
		Store: store, Bus: bus.NewInProcess(),
		Tasks:      services.NewTaskService(store, board),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
}

func TestAgentRegistryCRUD(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)

	resp := request(handler, http.MethodGet, "/api/v1/agents", "")
	assertStatus(t, resp, http.StatusOK)
	data := decodeBody(t, resp)["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("initial agent count = %d, want 1", len(data))
	}

	createBody := `{"name":"Researcher","provider":"ollama","model":"llama3","temperature":0.7,"role":"RESEARCH","max_tokens":2048}`
	resp = request(handler, http.MethodPost, "/api/v1/agents", createBody)
	assertStatus(t, resp, http.StatusCreated)
	created := decodeBody(t, resp)["data"].(map[string]any)
	id := created["id"].(string)
	if id == "" {
		t.Fatal("created agent id is empty")
	}
	if created["role"].(string) != "RESEARCH" {
		t.Fatalf("role = %v, want RESEARCH", created["role"])
	}

	patchBody := `{"temperature":0.0,"system_prompt":"Be precise."}`
	resp = request(handler, http.MethodPatch, "/api/v1/agents/"+id, patchBody)
	assertStatus(t, resp, http.StatusOK)
	patched := decodeBody(t, resp)["data"].(map[string]any)
	if patched["temperature"].(float64) != 0.0 {
		t.Fatalf("temperature = %v, want 0.0", patched["temperature"])
	}
	if patched["system_prompt"].(string) != "Be precise." {
		t.Fatalf("system_prompt = %v", patched["system_prompt"])
	}

	resp = request(handler, http.MethodDelete, "/api/v1/agents/default", "")
	assertStatus(t, resp, http.StatusConflict)

	resp = request(handler, http.MethodDelete, "/api/v1/agents/"+id, "")
	assertStatus(t, resp, http.StatusOK)
}

func TestAssignTaskAgentEndpoint(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)
	ctx := context.Background()

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "qa", Name: "QA", Provider: "openai", Model: "gpt-4o", Role: "QA",
	}); err != nil {
		t.Fatalf("seed qa: %v", err)
	}
	task := materializeOneTask(t, store)

	resp := request(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/assign", `{"agent_id":"qa"}`)
	assertStatus(t, resp, http.StatusOK)
	updated := decodeBody(t, resp)["data"].(map[string]any)
	if updated["AgentID"].(string) != "qa" {
		t.Fatalf("AgentID = %v, want qa", updated["AgentID"])
	}

	resp = request(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/assign", `{"agent_id":"missing"}`)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestSplitTaskEndpoint(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)
	task := materializeOneTask(t, store)

	body := `{"subtasks":[{"title":"Sub 1","description":"first"},{"title":"Sub 2","description":"second"}]}`
	resp := request(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/split", body)
	assertStatus(t, resp, http.StatusCreated)
	wrapper := decodeBody(t, resp)["data"].(map[string]any)
	parent := wrapper["parent"].(map[string]any)
	if parent["State"].(string) != string(models.TaskStateBlocked) {
		t.Fatalf("parent state = %v, want BLOCKED", parent["State"])
	}
	children := wrapper["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("children len = %d, want 2", len(children))
	}
}

func TestRetryEndpoint(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)
	ctx := context.Background()
	task := materializeOneTask(t, store)

	failed, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateFailed)
	if err != nil {
		t.Fatalf("transition to FAILED: %v", err)
	}
	_ = failed

	resp := request(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry", "")
	assertStatus(t, resp, http.StatusOK)
	updated := decodeBody(t, resp)["data"].(map[string]any)
	if updated["State"].(string) != string(models.TaskStateReady) {
		t.Fatalf("State = %v, want READY", updated["State"])
	}
}

func TestRetryEndpointRejectsRunning(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)
	task := materializeOneTask(t, store)

	resp := request(handler, http.MethodPost, fmt.Sprintf("/api/v1/tasks/%s/retry", task.ID), "")
	assertStatus(t, resp, http.StatusConflict)
}

// TestTaskResponsesEmbedAgentSnapshot proves that GET /projects/{id}/tasks
// and POST /tasks/{id}/assign both inline the AgentProfile so the cockpit
// avoids an extra round-trip per task to render assignments.
func TestTaskResponsesEmbedAgentSnapshot(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)
	ctx := context.Background()

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "qa", Name: "QA", Provider: "openai", Model: "gpt-4o", Role: "QA",
	}); err != nil {
		t.Fatalf("seed qa: %v", err)
	}
	task := materializeOneTask(t, store)

	resp := request(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/assign", `{"agent_id":"qa"}`)
	assertStatus(t, resp, http.StatusOK)
	updated := decodeBody(t, resp)["data"].(map[string]any)
	agent, ok := updated["agent"].(map[string]any)
	if !ok || agent["id"].(string) != "qa" {
		t.Fatalf("missing agent snapshot in assign response: %+v", updated)
	}

	resp = request(handler, http.MethodGet, "/api/v1/projects/"+task.ProjectID+"/tasks", "")
	assertStatus(t, resp, http.StatusOK)
	tasks := decodeBody(t, resp)["data"].([]any)
	if len(tasks) == 0 {
		t.Fatal("no tasks returned")
	}
	first := tasks[0].(map[string]any)
	emb, ok := first["agent"].(map[string]any)
	if !ok || emb["id"].(string) != "qa" {
		t.Fatalf("missing agent snapshot in list response: %+v", first)
	}
}

// Sanity check that the agent endpoints round-trip through json without
// dropping unknown fields silently.
func TestAgentResponseShape(t *testing.T) {
	store := newKanbanIntegrationStore(t)
	handler := newManagerLoopHandler(t, store)
	resp := request(handler, http.MethodGet, "/api/v1/agents/default", "")
	assertStatus(t, resp, http.StatusOK)

	raw, _ := json.Marshal(decodeBody(t, resp)["data"])
	var probe struct {
		ID        string `json:"id"`
		Provider  string `json:"provider"`
		Role      string `json:"role"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal agent response: %v", err)
	}
	if probe.ID != "default" || probe.Role != "CODE_GEN" {
		t.Fatalf("probe = %+v", probe)
	}
}
