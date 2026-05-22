package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type batchTestStore struct {
	project models.Project
	profile models.AgentProfile
	tasks   map[string]models.Task
	results map[string]*models.TaskResult
	mu      sync.Mutex
}

func (s *batchTestStore) lookupTaskLocked(id string) models.Task {
	if t, ok := s.tasks[id]; ok {
		return t
	}
	return models.Task{BaseEntity: models.BaseEntity{ID: id}, ProjectID: s.project.ID, AgentID: s.profile.ID}
}

func (s *batchTestStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, _ int) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.lookupTaskLocked(id)
	if t.State != models.TaskStateQueued {
		return nil, models.ErrStateConflict
	}
	t.State = models.TaskStateRunning
	s.tasks[id] = t
	return &t, nil
}

func (s *batchTestStore) UpdateTaskHeartbeat(context.Context, string) error { return nil }

func (s *batchTestStore) UpdateTaskResult(_ context.Context, id string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := result
	s.results[id] = &cp
	t := s.lookupTaskLocked(id)
	if result.Success {
		t.State = models.TaskStateCompleted
	} else {
		t.State = models.TaskStateFailed
	}
	s.tasks[id] = t
	return &t, nil
}

func (s *batchTestStore) GetProject(context.Context, string) (*models.Project, error) {
	return &s.project, nil
}

func (s *batchTestStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &s.profile, nil
}

func (s *batchTestStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.lookupTaskLocked(id)
	return &t, nil
}

func (s *batchTestStore) IncrementRetryCount(_ context.Context, id string, _ time.Time) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.lookupTaskLocked(id)
	t.RetryCount++
	s.tasks[id] = t
	return &t, nil
}
func (s *batchTestStore) UpdateTaskState(_ context.Context, id string, _ time.Time, next models.TaskState) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.lookupTaskLocked(id)
	t.State = next
	s.tasks[id] = t
	return &t, nil
}
func (s *batchTestStore) UpdateTaskDescription(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) AddComment(context.Context, models.Comment) error { return nil }
func (s *batchTestStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, nil
}
func (s *batchTestStore) ListCommentsSince(context.Context, string, time.Time) ([]models.Comment, error) {
	return nil, nil
}
func (s *batchTestStore) Close() error                                    { return nil }
func (s *batchTestStore) AppendEvent(context.Context, models.Event) error { return nil }
func (s *batchTestStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (s *batchTestStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *batchTestStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *batchTestStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *batchTestStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (s *batchTestStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *batchTestStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *batchTestStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *batchTestStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *batchTestStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }
func (s *batchTestStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{s.profile}, nil
}
func (s *batchTestStore) DeleteAgentProfile(context.Context, string) error { return nil }
func (s *batchTestStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ListSettings(context.Context) ([]models.Setting, error) { return nil, nil }
func (s *batchTestStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (s *batchTestStore) SetSetting(context.Context, string, string) error { return nil }
func (s *batchTestStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}
func (s *batchTestStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &s.project, nil
}
func (s *batchTestStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}
func (s *batchTestStore) ListProjects(context.Context) ([]models.Project, error) { return nil, nil }
func (s *batchTestStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ReconcileStaleTasks(context.Context, []int, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) BlockTaskWithSubtasks(context.Context, string, time.Time, []models.DraftTask) (*models.Task, []models.Task, error) {
	return nil, nil, nil
}
func (s *batchTestStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
}
func (s *batchTestStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (s *batchTestStore) MarkCommentProcessed(context.Context, string, string) error { return nil }

type batchTrackingGateway struct {
	mu            sync.Mutex
	generateCount int
	batchCalls    int
	singleCalls   int
	omitSlot      *int // when non-nil, batch response omits this slot index
}

func (g *batchTrackingGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generateCount++

	isBatch := false
	for _, m := range req.Messages {
		if m.Role == "user" && countBatchSlots(m.Content) > 1 {
			isBatch = true
			break
		}
	}
	if isBatch {
		g.batchCalls++
		return gateway.AIResponse{Content: g.batchJSON(req)}, nil
	}
	g.singleCalls++
	if g.isLegacyRequest(req) {
		return gateway.AIResponse{Content: `{"command":"echo ok"}`}, nil
	}
	return gateway.AIResponse{Content: "single fallback summary"}, nil
}

func (g *batchTrackingGateway) isLegacyRequest(req gateway.AIRequest) bool {
	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, batchLegacySystemSuffix) {
			return true
		}
		if m.Role == "system" && strings.Contains(m.Content, legacyJSONCommandSystemSentinel) {
			return true
		}
	}
	return false
}

func (g *batchTrackingGateway) batchJSON(req gateway.AIRequest) string {
	slotCount := 0
	for _, m := range req.Messages {
		if m.Role == "user" {
			slotCount = countBatchSlots(m.Content)
			break
		}
	}
	if slotCount == 0 {
		slotCount = 1
	}

	if strings.Contains(req.Messages[0].Content, batchLegacySystemSuffix) {
		var results []batchLegacySlot
		for i := 0; i < slotCount; i++ {
			if g.omitSlot != nil && *g.omitSlot == i {
				continue
			}
			results = append(results, batchLegacySlot{Slot: i, Command: "echo ok"})
		}
		b, _ := json.Marshal(batchLegacyResponse{Results: results})
		return string(b)
	}

	var results []batchTextSlot
	for i := 0; i < slotCount; i++ {
		if g.omitSlot != nil && *g.omitSlot == i {
			continue
		}
		results = append(results, batchTextSlot{Slot: i, Content: fmt.Sprintf("summary-%d", i)})
	}
	b, _ := json.Marshal(batchTextResponse{Results: results})
	return string(b)
}

type batchFailingGateway struct{}

func (g *batchFailingGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	for _, m := range req.Messages {
		if m.Role == "user" && countBatchSlots(m.Content) > 1 {
			return gateway.AIResponse{}, errors.New("batch gateway unavailable")
		}
	}
	return gateway.AIResponse{Content: "single fallback summary"}, nil
}

func (g *batchFailingGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *batchFailingGateway) AnalyzeScope(context.Context, string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}
func (g *batchFailingGateway) ClassifyIntent(context.Context, string) (*spec.IntentAnalysis, error) {
	return nil, nil
}
func (g *batchFailingGateway) Embed(context.Context, spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.EmbedResponse{}, nil
}

func (g *batchTrackingGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *batchTrackingGateway) AnalyzeScope(context.Context, string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}
func (g *batchTrackingGateway) ClassifyIntent(context.Context, string) (*spec.IntentAnalysis, error) {
	return nil, nil
}
func (g *batchTrackingGateway) Embed(context.Context, spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.EmbedResponse{}, nil
}

type batchCountingSandbox struct {
	mu    sync.Mutex
	calls int
}

func (s *batchCountingSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}, nil
}

func summarizeTasks(n int) []models.Task {
	tasks := make([]models.Task, n)
	for i := 0; i < n; i++ {
		tasks[i] = models.Task{
			BaseEntity:  models.BaseEntity{ID: fmt.Sprintf("t%d", i+1)},
			ProjectID:   "p1",
			AgentID:     "ag1",
			Title:       "Summarize report section",
			Description: "Provide a short recap and condense the notes",
			State:       models.TaskStateQueued,
		}
	}
	return tasks
}
