package worker_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"agentd/internal/bus"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/queue/worker"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// Plan-and-execute end to end, in process: a real kanban store, worker and
// bash sandbox, with the gateway's openai adapter talking HTTP to a fake
// OpenAI-compatible server. It replaces the former docker compose scenario
// (agentd -> litellm -> mock LLM) and checks two things only the wire shows:
// a broken-down parent is rolled up instead of re-decomposed on resume, and
// send_task_metadata puts the right task_id/agent_id/role on every request.

const planMarker = "AGENT_PLAN"

var taskTitlePattern = regexp.MustCompile(`Task:\s*(.+)`)

type fakeChatRequest struct {
	Title    string
	Metadata map[string]string
}

// fakeOpenAI decomposes plan-marked tasks and answers everything else with a
// shell command that records the request's metadata task_id as evidence.
type fakeOpenAI struct {
	mu       sync.Mutex
	requests []fakeChatRequest
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var title string
	for _, m := range body.Messages {
		if match := taskTitlePattern.FindStringSubmatch(m.Content); m.Role == "user" && match != nil {
			title = strings.TrimSpace(match[1])
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, fakeChatRequest{Title: title, Metadata: body.Metadata})
	f.mu.Unlock()

	var content any
	if strings.Contains(title, planMarker) {
		base := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(title, planMarker), ":"))
		content = map[string]any{"too_complex": true, "subtasks": []map[string]string{
			{"title": base + " :: Step 1 - scaffold", "description": "Create the scaffold for the requested work."},
			{"title": base + " :: Step 2 - implement", "description": "Implement the core of the requested work."},
		}}
	} else {
		taskID := body.Metadata["task_id"]
		if taskID == "" {
			taskID = "unknown"
		}
		content = map[string]string{"command": fmt.Sprintf("echo 'task=%s executed' >> PLAN_RESULTS.log", taskID)}
	}
	encoded, _ := json.Marshal(content)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": "agentd",
		"choices": []map[string]any{{
			"message": map[string]string{"role": "assistant", "content": string(encoded)},
		}},
		"usage": map[string]int{"total_tokens": 2},
	})
}

func (f *fakeOpenAI) snapshot() []fakeChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeChatRequest(nil), f.requests...)
}

func seedDefaultProfile(t *testing.T, store models.KanbanStore) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "default", Name: "Default Coding Agent", Temperature: 0.2, MaxTokens: 1024, Role: "CODE_GEN",
		SystemPrompt: sql.NullString{String: "Suggest one safe shell command for the requested task. Output only JSON.", Valid: true},
	}); err != nil {
		t.Fatalf("seed default profile: %v", err)
	}
}

func buildOpenAIAdapterRouter(t *testing.T, baseURL string) *gateway.Router {
	t.Helper()
	router, err := gateway.NewRouterFromConfigs([]spec.ProviderConfig{{
		Name: "litellm", Adapter: "openai", BaseURL: baseURL + "/v1", APIKey: "sk-test", Model: "agentd",
		Options: map[string]any{"send_task_metadata": true},
	}})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	return router
}

func materializePlan(t *testing.T, store models.KanbanStore, ctx context.Context, projectsDir string) *models.Project {
	t.Helper()
	// The fake store does not scaffold the workspace root the way the real
	// one does, and the worker refuses to run a project without it.
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	project, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "plan-execute-demo",
		Tasks: []models.DraftTask{
			{TempID: "greet", Title: "Generate a greeting script", Description: "Write a hello script to the workspace.", AgentID: "default"},
			{TempID: "plan", Title: planMarker + ": Produce a status report", Description: "A small report task the agent should decompose.", AgentID: "default"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if err := os.MkdirAll(project.WorkspacePath, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return project
}

func verifyPlanExecution(t *testing.T, ctx context.Context, store models.KanbanStore, tasks []models.Task, project *models.Project, fake *fakeOpenAI) {
	t.Helper()
	byTitle, parent := verifyPlanTasks(t, ctx, store, tasks)
	verifyPlanRequests(t, byTitle, parent, fake)
	verifyPlanEvidence(t, tasks, parent, project)
}

func verifyPlanTasks(t *testing.T, ctx context.Context, store models.KanbanStore, tasks []models.Task) (map[string]models.Task, models.Task) {
	t.Helper()
	byTitle := make(map[string]models.Task, len(tasks))
	var parent models.Task
	steps := 0
	for _, task := range tasks {
		byTitle[task.Title] = task
		if task.State != models.TaskStateCompleted {
			t.Errorf("task %q state = %s, want COMPLETED", task.Title, task.State)
		}
		if strings.Contains(task.Title, planMarker) {
			parent = task
		}
		if strings.Contains(task.Title, ":: Step") {
			steps++
		}
	}
	if len(tasks) != 4 {
		t.Fatalf("tasks = %d, want 4 (2 materialized + 2 subtasks): %v", len(tasks), taskSummaries(tasks))
	}
	if parent.ID == "" {
		t.Fatal("AGENT_PLAN parent task missing")
	}
	children, err := store.ListChildTasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if steps != 2 || len(children) != 2 {
		t.Fatalf("subtasks: %d titled ':: Step', %d children of the parent; want 2 and 2", steps, len(children))
	}
	return byTitle, parent
}

func verifyPlanRequests(t *testing.T, byTitle map[string]models.Task, parent models.Task, fake *fakeOpenAI) {
	t.Helper()
	requests := fake.snapshot()
	decomposes, parentRequests := 0, 0
	for i, req := range requests {
		if strings.Contains(req.Title, planMarker) {
			decomposes++
		}
		task, ok := byTitle[req.Title]
		if !ok {
			t.Errorf("request %d: prompt title %q matches no task", i, req.Title)
			continue
		}
		if task.ID == parent.ID {
			parentRequests++
		}
		want := map[string]string{"task_id": task.ID, "agent_id": "default", "role": string(spec.RoleWorker)}
		for key, value := range want {
			if req.Metadata[key] != value {
				t.Errorf("request %d (%q): metadata[%s] = %q, want %q (metadata=%v)", i, req.Title, key, req.Metadata[key], value, req.Metadata)
			}
		}
	}
	if decomposes != 1 {
		t.Errorf("decompose requests = %d, want exactly 1", decomposes)
	}
	if parentRequests != 1 {
		t.Errorf("requests for the parent = %d, want 1 (it must not be re-sent after the split)", parentRequests)
	}
}

func verifyPlanEvidence(t *testing.T, tasks []models.Task, parent models.Task, project *models.Project) {
	t.Helper()
	evidence, err := os.ReadFile(filepath.Join(project.WorkspacePath, "PLAN_RESULTS.log"))
	if err != nil {
		t.Fatalf("read PLAN_RESULTS.log: %v", err)
	}
	for _, task := range tasks {
		line := "task=" + task.ID + " executed"
		if task.ID == parent.ID {
			if strings.Contains(string(evidence), line) {
				t.Errorf("parent %s was executed; it should only be rolled up", task.ID)
			}
			continue
		}
		if !strings.Contains(string(evidence), line) {
			t.Errorf("PLAN_RESULTS.log has no entry for %q (%s):\n%s", task.Title, task.ID, evidence)
		}
	}
	if strings.Contains(string(evidence), "task=unknown") {
		t.Errorf("PLAN_RESULTS.log has uncorrelated entries:\n%s", evidence)
	}
}

func TestPlanExecute_ThroughOpenAIAdapter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	store := testutil.NewFakeStore()
	t.Cleanup(func() { _ = store.Close() })
	store.SetProjectsDir(projectsDir)
	seedDefaultProfile(t, store)

	fake := &fakeOpenAI{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	router := buildOpenAIAdapterRouter(t, server.URL)
	emitter := bus.NewEventEmitter(store, bus.NewInProcess())
	sb := &sandbox.BashExecutor{Root: projectsDir, Sink: bus.EventBridge{Emitter: emitter}}
	w := worker.NewWorker(store, router, sb, nil, emitter, worker.WorkerOptions{ProjectsDir: projectsDir})

	project := materializePlan(t, store, ctx, projectsDir)
	tasks := drivePlanToRest(t, store, w, project.ID)
	verifyPlanExecution(t, ctx, store, tasks, project, fake)
}

// drivePlanToRest runs the daemon's claim-and-process cycle until no task in
// the project is PENDING, READY, QUEUED or RUNNING, failing if it doesn't
// settle within a bounded number of rounds.
func drivePlanToRest(t *testing.T, store models.KanbanStore, w *worker.Worker, projectID string) []models.Task {
	t.Helper()
	ctx := context.Background()
	const maxRounds = 20
	for round := 0; round < maxRounds; round++ {
		tasks, err := store.ListTasksByProject(ctx, projectID)
		if err != nil {
			t.Fatalf("ListTasksByProject: %v", err)
		}
		active := false
		for _, task := range tasks {
			switch task.State {
			case models.TaskStatePending, models.TaskStateReady, models.TaskStateQueued, models.TaskStateRunning:
				active = true
			}
		}
		if !active {
			return tasks
		}
		claimed, err := store.ClaimNextReadyTasks(ctx, 4)
		if err != nil {
			t.Fatalf("ClaimNextReadyTasks: %v", err)
		}
		if len(claimed) == 0 {
			t.Fatalf("round %d: active tasks but none claimable: %v", round, taskSummaries(tasks))
		}
		for _, task := range claimed {
			w.Process(ctx, task)
		}
	}
	tasks, _ := store.ListTasksByProject(ctx, projectID)
	t.Fatalf("plan did not settle within %d rounds (%d tasks): %v", maxRounds, len(tasks), taskSummaries(tasks))
	return nil
}

func taskSummaries(tasks []models.Task) []string {
	out := make([]string, len(tasks))
	for i, task := range tasks {
		out[i] = fmt.Sprintf("%s[%s]", task.Title, task.State)
	}
	return out
}
