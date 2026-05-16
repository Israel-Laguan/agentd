package queue

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type fakeGateway struct {
	mu             sync.Mutex
	content        string
	nextContent    string
	err            error
	requests       []gateway.AIRequest
	plan           *models.DraftPlan
	planCalls      int
	lastPlanIntent string
	toolCalls      []gateway.ToolCall
	nextToolCalls  []gateway.ToolCall
}

func (g *fakeGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
	content := g.content
	var toolCalls []gateway.ToolCall
	requestNum := len(g.requests)

	if requestNum == 1 {
		toolCalls = g.toolCalls
	} else {
		if g.nextContent != "" {
			content = g.nextContent
		}
		toolCalls = g.nextToolCalls
	}

	if len(toolCalls) == 0 && requestNum == 1 && g.content != "" {
		return gateway.AIResponse{Content: content}, g.err
	}
	return gateway.AIResponse{Content: content, ToolCalls: toolCalls}, g.err
}

func (g *fakeGateway) GeneratePlan(_ context.Context, intent string) (*models.DraftPlan, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.planCalls++
	g.lastPlanIntent = intent
	if g.plan != nil {
		return g.plan, g.err
	}
	return &models.DraftPlan{Tasks: []models.DraftTask{{Title: "follow"}}}, g.err
}

func (*fakeGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (*fakeGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func (s *recordingSink) hasEvent(kind string) bool {
	for _, event := range s.events {
		if string(event.Type) == kind {
			return true
		}
	}
	return false
}

type fakeSandbox struct {
	mu       sync.Mutex
	result   sandbox.Result
	err      error
	results  []sandbox.Result
	errs     []error
	commands []string
	payloads []sandbox.Payload
	delay    time.Duration
}

func (s *fakeSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	s.mu.Lock()
	s.commands = append(s.commands, payload.Command)
	s.payloads = append(s.payloads, payload)
	delay := s.delay
	s.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return sandbox.Result{Success: false, ExitCode: -1}, ctx.Err()
		case <-timer.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.results) > 0 {
		result := s.results[0]
		s.results = s.results[1:]
		var err error
		if len(s.errs) > 0 {
			err = s.errs[0]
			s.errs = s.errs[1:]
		}
		return result, err
	}
	return s.result, s.err
}

type workerStore struct {
	task       models.Task
	project    models.Project
	profile    models.AgentProfile
	result     *models.TaskResult
	comment    string
	comments   []models.Comment
	appends    int
	drafts     []models.DraftTask
	tasks      []models.Task
	heartbeats int
}

func newWorkerStore() *workerStore {
	now := time.Now().UTC()
	return &workerStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "task", UpdatedAt: now},
			ProjectID:  "project", AgentID: "default", State: models.TaskStateQueued,
		},
		project: models.Project{BaseEntity: models.BaseEntity{ID: "project"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "default", Temperature: 0.2, SystemPrompt: sql.NullString{
			String: "Return JSON.", Valid: true,
		}},
	}
}

func (s *workerStore) MarkTaskRunning(_ context.Context, _ string, _ time.Time, pid int) (*models.Task, error) {
	s.task.State = models.TaskStateRunning
	s.task.UpdatedAt = s.task.UpdatedAt.Add(time.Second)
	s.task.OSProcessID = &pid
	now := time.Now().UTC()
	s.task.LastHeartbeat = &now
	return &s.task, nil
}

func (s *workerStore) UpdateTaskHeartbeat(context.Context, string) error {
	s.heartbeats++
	now := time.Now().UTC()
	s.task.LastHeartbeat = &now
	return nil
}

func (s *workerStore) IncrementRetryCount(context.Context, string, time.Time) (*models.Task, error) {
	s.task.RetryCount++
	s.task.UpdatedAt = s.task.UpdatedAt.Add(time.Second)
	return &s.task, nil
}

func (s *workerStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, next models.TaskState) (*models.Task, error) {
	s.task.State = next
	s.task.UpdatedAt = s.task.UpdatedAt.Add(time.Second)
	return &s.task, nil
}

func (s *workerStore) UpdateTaskResult(_ context.Context, _ string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	s.result = &result
	if result.Success {
		s.task.State = models.TaskStateCompleted
	} else {
		s.task.State = models.TaskStateFailed
	}
	return &s.task, nil
}

func (s *workerStore) AddComment(_ context.Context, c models.Comment) error {
	s.comment = c.Body
	return nil
}
func (s *workerStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return append([]models.Comment(nil), s.comments...), nil
}
func (s *workerStore) ListCommentsSince(_ context.Context, _ string, since time.Time) ([]models.Comment, error) {
	var out []models.Comment
	for _, c := range s.comments {
		if since.IsZero() || c.UpdatedAt.After(since) {
			out = append(out, c)
		}
	}
	return out, nil
}
func (s *workerStore) GetProject(context.Context, string) (*models.Project, error) {
	return &s.project, nil
}
func (s *workerStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &s.profile, nil
}
func (s *workerStore) GetTask(context.Context, string) (*models.Task, error) { return &s.task, nil }
func (s *workerStore) Close() error                                          { return nil }
func (s *workerStore) AppendEvent(context.Context, models.Event) error       { return nil }
func (s *workerStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (s *workerStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *workerStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *workerStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *workerStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *workerStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (s *workerStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *workerStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *workerStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *workerStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *workerStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }
func (s *workerStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{s.profile}, nil
}
func (s *workerStore) DeleteAgentProfile(context.Context, string) error { return nil }
func (s *workerStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return &s.task, nil
}
func (s *workerStore) ListSettings(context.Context) ([]models.Setting, error) { return nil, nil }
func (s *workerStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (s *workerStore) SetSetting(context.Context, string, string) error { return nil }
func (s *workerStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, errors.New("not implemented")
}
func (s *workerStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{BaseEntity: models.BaseEntity{ID: "system"}, Name: "_system"}, nil
}
func (s *workerStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: "system-task"}, ProjectID: projectID, Title: draft.Title, Description: draft.Description, Assignee: draft.Assignee}, true, nil
}
func (s *workerStore) ListProjects(context.Context) ([]models.Project, error) { return nil, nil }
func (s *workerStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	if s.tasks != nil {
		return append([]models.Task(nil), s.tasks...), nil
	}
	return nil, nil
}
func (s *workerStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}
func (s *workerStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *workerStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (s *workerStore) ReconcileStaleTasks(context.Context, []int, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *workerStore) AppendTasksToProject(_ context.Context, _ string, _ string, drafts []models.DraftTask) ([]models.Task, error) {
	s.appends++
	s.drafts = append(s.drafts, drafts...)
	return []models.Task{{BaseEntity: models.BaseEntity{ID: "child"}}}, nil
}
func (s *workerStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, drafts []models.DraftTask) (*models.Task, []models.Task, error) {
	s.appends++
	s.drafts = append(s.drafts, drafts...)
	s.task.State = models.TaskStateBlocked
	s.task.UpdatedAt = s.task.UpdatedAt.Add(time.Second)
	children := make([]models.Task, 0, len(drafts))
	for _, draft := range drafts {
		children = append(children, models.Task{
			BaseEntity:  models.BaseEntity{ID: "child-" + draft.Title},
			Title:       draft.Title,
			Description: draft.Description,
			State:       models.TaskStateReady,
			Assignee:    draft.Assignee,
		})
	}
	return &s.task, children, nil
}

func (s *workerStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (s *workerStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
}
func (s *workerStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (s *workerStore) MarkCommentProcessed(context.Context, string, string) error {
	s.task.Assignee = models.TaskAssigneeSystem
	return nil
}
