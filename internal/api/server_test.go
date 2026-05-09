package api_test

import (
	"context"
	"database/sql"
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

func TestUnifiedProjectResponses(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})
	resp := request(handler, http.MethodGet, "/api/v1/projects", "")
	assertStatus(t, resp, http.StatusOK)
	assertJSONField(t, resp, "status", "success")
	if _, ok := decodeBody(t, resp)["meta"].(map[string]any); !ok {
		t.Fatal("response missing meta object")
	}

	resp = request(handler, http.MethodGet, "/api/v1/projects/invalid-id", "")
	assertStatus(t, resp, http.StatusNotFound)
	assertNestedField(t, resp, "error", "code", "NOT_FOUND")
}

func TestOpenAICompatibleIntake(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})
	body := `{"model":"agentd","messages":[{"role":"user","content":"A Python script to scrape a website"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	decoded := decodeBody(t, resp)
	if decoded["object"] != "chat.completion" {
		t.Fatalf("object = %v", decoded["object"])
	}
	content := decoded["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !strings.Contains(content, "Python scraper") {
		t.Fatalf("content missing DraftPlan: %s", content)
	}
}

func TestHumanCommentCancelsRunningTask(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})
	resp := request(handler, http.MethodPost, "/api/v1/tasks/123/comments", `{"content":"Stop, use python 3"}`)
	assertStatus(t, resp, http.StatusCreated)
	task, err := store.GetTask(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != models.TaskStateInConsideration {
		t.Fatalf("state=%s, want IN_CONSIDERATION", task.State)
	}
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

func assertNestedField(t *testing.T, resp *httptest.ResponseRecorder, object, key string, want any) {
	t.Helper()
	got := decodeBody(t, resp)[object].(map[string]any)[key]
	if got != want {
		t.Fatalf("%s.%s = %v, want %v", object, key, got, want)
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

func (g *apiGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}

func (g *apiGateway) GeneratePlan(_ context.Context, intent string) (*models.DraftPlan, error) {
	g.planCalls++
	g.lastPlanIntent = intent
	if g.planErr != nil {
		return nil, g.planErr
	}
	if g.plan != nil {
		return g.plan, nil
	}
	return &models.DraftPlan{ProjectName: "Python scraper", Tasks: []models.DraftTask{{TempID: "a", Title: "Scrape site"}}}, nil
}

func (g *apiGateway) AnalyzeScope(_ context.Context, intent string) (*gateway.ScopeAnalysis, error) {
	g.analyzeCalls++
	g.lastAnalyzeIntent = intent
	if g.scopeErr != nil {
		return nil, g.scopeErr
	}
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

func (g *apiGateway) ClassifyIntent(_ context.Context, intent string) (*gateway.IntentAnalysis, error) {
	g.intentCalls++
	g.lastClassifyIntent = intent
	if g.intentErr != nil {
		return nil, g.intentErr
	}
	if g.intent != nil {
		return g.intent, nil
	}
	return &gateway.IntentAnalysis{
		Intent: "plan_request",
		Reason: "default test behavior",
	}, nil
}

type apiGateway struct {
	scope              *gateway.ScopeAnalysis
	scopeErr           error
	analyzeCalls       int
	intent             *gateway.IntentAnalysis
	intentErr          error
	intentCalls        int
	lastClassifyIntent string
	lastAnalyzeIntent  string
	planCalls          int
	lastPlanIntent     string
	plan               *models.DraftPlan
	planErr            error
}

func newAPIGateway() *apiGateway { return &apiGateway{} }

type apiStore struct {
	mu       sync.Mutex
	project  models.Project
	task     models.Task
	comments []models.Comment
}

func newAPITestStore() *apiStore {
	now := time.Now().UTC()
	return &apiStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "project", CreatedAt: now, UpdatedAt: now}, Name: "Project"},
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "123", CreatedAt: now, UpdatedAt: now},
			ProjectID:  "project", State: models.TaskStateRunning, Assignee: models.TaskAssigneeSystem,
		},
	}
}

func (s *apiStore) Close() error { return nil }
func (s *apiStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return &s.project, []models.Task{s.task}, nil
}
func (s *apiStore) GetProject(_ context.Context, id string) (*models.Project, error) {
	if id != s.project.ID {
		return nil, models.ErrProjectNotFound
	}
	return &s.project, nil
}
func (s *apiStore) ListProjects(context.Context) ([]models.Project, error) {
	return []models.Project{s.project}, nil
}
func (s *apiStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	if id != s.task.ID {
		return nil, models.ErrTaskNotFound
	}
	return &s.task, nil
}
func (s *apiStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *apiStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) { return nil, nil }
func (s *apiStore) MarkTaskRunning(context.Context, string, time.Time, int) (*models.Task, error) {
	return &s.task, nil
}
func (s *apiStore) UpdateTaskHeartbeat(context.Context, string) error {
	return nil
}
func (s *apiStore) IncrementRetryCount(context.Context, string, time.Time) (*models.Task, error) {
	return &s.task, nil
}
func (s *apiStore) UpdateTaskState(context.Context, string, time.Time, models.TaskState) (*models.Task, error) {
	return &s.task, nil
}
func (s *apiStore) UpdateTaskResult(context.Context, string, time.Time, models.TaskResult) (*models.Task, error) {
	return &s.task, nil
}
func (s *apiStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *apiStore) ReconcileStaleTasks(context.Context, []int, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *apiStore) BlockTaskWithSubtasks(context.Context, string, time.Time, []models.DraftTask) (*models.Task, []models.Task, error) {
	return &s.task, nil, nil
}
func (s *apiStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}
func (s *apiStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{BaseEntity: models.BaseEntity{ID: "system"}, Name: "_system"}, nil
}
func (s *apiStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: "system-task"}, ProjectID: projectID, Title: draft.Title, Description: draft.Description, Assignee: draft.Assignee}, true, nil
}
func (s *apiStore) AddComment(_ context.Context, c models.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments = append(s.comments, c)
	s.task.State = models.TaskStateInConsideration
	return nil
}
func (s *apiStore) ListComments(context.Context, string) ([]models.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Comment(nil), s.comments...), nil
}
func (s *apiStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (s *apiStore) MarkCommentProcessed(context.Context, string, string) error { return nil }
func (s *apiStore) AppendEvent(context.Context, models.Event) error            { return nil }
func (s *apiStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (s *apiStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *apiStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *apiStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *apiStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *apiStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (s *apiStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *apiStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *apiStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *apiStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *apiStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return nil, nil
}
func (s *apiStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }
func (s *apiStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return nil, nil
}
func (s *apiStore) DeleteAgentProfile(context.Context, string) error { return nil }
func (s *apiStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (s *apiStore) ListSettings(context.Context) ([]models.Setting, error)   { return nil, nil }
func (s *apiStore) GetSetting(context.Context, string) (string, bool, error) { return "", false, nil }
func (s *apiStore) SetSetting(context.Context, string, string) error         { return nil }

var _ models.KanbanStore = (*apiStore)(nil)
var _ = sql.ErrNoRows
