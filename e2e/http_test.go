package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestSystemStatus(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/system/status", "")
	assertStatus(t, resp, http.StatusOK)
	assertJSONField(t, resp, "status", "success")
}

func TestProjectsList(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/projects", "")
	assertStatus(t, resp, http.StatusOK)
	assertJSONField(t, resp, "status", "success")
	body := decodeBody(t, resp)
	if _, ok := body["data"].([]any); !ok {
		t.Fatal("data is not array")
	}
	if _, ok := body["meta"].(map[string]any); !ok {
		t.Fatal("meta missing")
	}
}

func TestAgentsList(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/agents", "")
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	data := body["data"].([]any)
	if len(data) < 1 {
		t.Fatal("no agents returned")
	}
}

func TestAgentsDefault(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/agents/default", "")
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	if data["id"] != "default" {
		t.Fatalf("agent id = %v, want default", data["id"])
	}
}

func TestAgentsQA(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/agents/qa", "")
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	if data["id"] != "qa" {
		t.Fatalf("agent id = %v, want qa", data["id"])
	}
}

func TestAgentsResearcher(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/agents/researcher", "")
	assertStatus(t, resp, http.StatusOK)
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	if data["id"] != "researcher" {
		t.Fatalf("agent id = %v, want researcher", data["id"])
	}
}

func TestChatCompletion(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{"messages":[{"role":"user","content":"hello"}],"stream":false}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	bodyResp := decodeBody(t, resp)
	if bodyResp["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion", bodyResp["object"])
	}
}

func TestRapidStatusRequests(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	ok := 0
	for i := 0; i < 5; i++ {
		resp := request(handler, http.MethodGet, "/api/v1/system/status", "")
		if resp.Code == http.StatusOK {
			body := decodeBody(t, resp)
			if body["status"] == "success" {
				ok++
			}
		}
	}
	if ok != 5 {
		t.Fatalf("rapid requests: %d/5 successful", ok)
	}
}

func TestNotFound(t *testing.T) {
	store := newTestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Gateway:    newTestGateway(),
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	resp := request(handler, http.MethodGet, "/api/v1/nonexistent", "")
	assertStatus(t, resp, http.StatusNotFound)
}

func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertStatus(t *testing.T, resp *httptest.ResponseRecorder, want int) {
	t.Helper()
	if resp.Code != want {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, want, resp.Body.String())
	}
}

func assertJSONField(t *testing.T, resp *httptest.ResponseRecorder, key string, want any) {
	t.Helper()
	if got := decodeBody(t, resp)[key]; got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}

func decodeBody(t *testing.T, resp *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return decoded
}

type testStore struct {
	mu       sync.Mutex
	project  models.Project
	task     models.Task
	comments []models.Comment
}

func newTestStore() *testStore {
	now := time.Now().UTC()
	return &testStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "project", CreatedAt: now, UpdatedAt: now}, Name: "Project"},
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "123", CreatedAt: now, UpdatedAt: now},
			ProjectID:  "project", State: models.TaskStateRunning, Assignee: models.TaskAssigneeSystem,
		},
	}
}

func (s *testStore) Close() error { return nil }
func (s *testStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return &s.project, []models.Task{s.task}, nil
}
func (s *testStore) GetProject(_ context.Context, id string) (*models.Project, error) {
	if id != s.project.ID {
		return nil, models.ErrProjectNotFound
	}
	return &s.project, nil
}
func (s *testStore) ListProjects(context.Context) ([]models.Project, error) {
	return []models.Project{s.project}, nil
}
func (s *testStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	if id != s.task.ID {
		return nil, models.ErrTaskNotFound
	}
	return &s.task, nil
}
func (s *testStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *testStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) { return nil, nil }
func (s *testStore) MarkTaskRunning(context.Context, string, time.Time, int) (*models.Task, error) {
	return &s.task, nil
}
func (s *testStore) UpdateTaskHeartbeat(context.Context, string) error { return nil }
func (s *testStore) IncrementRetryCount(context.Context, string, time.Time) (*models.Task, error) {
	return &s.task, nil
}
func (s *testStore) UpdateTaskState(context.Context, string, time.Time, models.TaskState) (*models.Task, error) {
	return &s.task, nil
}
func (s *testStore) UpdateTaskResult(context.Context, string, time.Time, models.TaskResult) (*models.Task, error) {
	return &s.task, nil
}
func (s *testStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *testStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (s *testStore) ReconcileStaleTasks(context.Context, []int, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *testStore) BlockTaskWithSubtasks(context.Context, string, time.Time, []models.DraftTask) (*models.Task, []models.Task, error) {
	return &s.task, nil, nil
}
func (s *testStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}
func (s *testStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{BaseEntity: models.BaseEntity{ID: "system"}, Name: "_system"}, nil
}
func (s *testStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: "system-task"}, ProjectID: projectID, Title: draft.Title, Description: draft.Description, Assignee: draft.Assignee}, true, nil
}
func (s *testStore) AddComment(_ context.Context, c models.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments = append(s.comments, c)
	s.task.State = models.TaskStateInConsideration
	return nil
}
func (s *testStore) ListComments(context.Context, string) ([]models.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Comment(nil), s.comments...), nil
}
func (s *testStore) ListCommentsSince(_ context.Context, _ string, since time.Time) ([]models.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Comment
	for _, c := range s.comments {
		if since.IsZero() || c.UpdatedAt.After(since) {
			out = append(out, c)
		}
	}
	return out, nil
}
func (s *testStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (s *testStore) MarkCommentProcessed(context.Context, string, string) error { return nil }
func (s *testStore) AppendEvent(context.Context, models.Event) error            { return nil }
func (s *testStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (s *testStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *testStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *testStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *testStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *testStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (s *testStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *testStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *testStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *testStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *testStore) GetAgentProfile(_ context.Context, id string) (*models.AgentProfile, error) {
	switch id {
	case "default":
		return &models.AgentProfile{ID: "default", Name: "Default Agent", Provider: "openai", Model: "gpt-4"}, nil
	case "qa":
		return &models.AgentProfile{ID: "qa", Name: "QA Agent", Provider: "openai", Model: "gpt-4"}, nil
	case "researcher":
		return &models.AgentProfile{ID: "researcher", Name: "Researcher Agent", Provider: "openai", Model: "gpt-4"}, nil
	}
	return nil, nil
}
func (s *testStore) UpsertAgentProfile(_ context.Context, profile models.AgentProfile) error {
	return nil
}
func (s *testStore) ListAgentProfiles(_ context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{
		{ID: "default", Name: "Default Agent", Provider: "openai", Model: "gpt-4"},
		{ID: "qa", Name: "QA Agent", Provider: "openai", Model: "gpt-4"},
		{ID: "researcher", Name: "Researcher Agent", Provider: "openai", Model: "gpt-4"},
	}, nil
}
func (s *testStore) DeleteAgentProfile(_ context.Context, id string) error {
	return nil
}
func (s *testStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (s *testStore) ListSettings(context.Context) ([]models.Setting, error)   { return nil, nil }
func (s *testStore) GetSetting(context.Context, string) (string, bool, error) { return "", false, nil }
func (s *testStore) SetSetting(context.Context, string, string) error         { return nil }

var _ models.KanbanStore = (*testStore)(nil)

type testGateway struct {
	scope  *gateway.ScopeAnalysis
	intent *gateway.IntentAnalysis
	plan   *models.DraftPlan
}

func newTestGateway() *testGateway { return &testGateway{} }

func (g *testGateway) Generate(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}

func (g *testGateway) GeneratePlan(_ context.Context, intent string) (*models.DraftPlan, error) {
	if g.plan != nil {
		return g.plan, nil
	}
	return &models.DraftPlan{ProjectName: "Python scraper", Tasks: []models.DraftTask{{TempID: "a", Title: "Scrape site"}}}, nil
}

func (g *testGateway) AnalyzeScope(_ context.Context, intent string) (*gateway.ScopeAnalysis, error) {
	if g.scope != nil {
		return g.scope, nil
	}
	return &gateway.ScopeAnalysis{
		SingleScope: true,
		Confidence:  1,
		Scopes:      []gateway.ScopeOption{{ID: "input", Label: "Input request"}},
		Reason:      "single request",
	}, nil
}

func (g *testGateway) ClassifyIntent(_ context.Context, intent string) (*gateway.IntentAnalysis, error) {
	if g.intent != nil {
		return g.intent, nil
	}
	return &gateway.IntentAnalysis{
		Intent: "plan_request",
		Reason: "default test behavior",
	}, nil
}
