package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

type dispatchBatchStore struct {
	mu       sync.Mutex
	tasks    []models.Task
	projects map[string]models.Project
	profiles map[string]models.AgentProfile
	events   []models.TokenUsageEvent
	queries  int
}

func newDispatchBatchStore(tasks []models.Task) *dispatchBatchStore {
	projects := map[string]models.Project{
		"p1": {BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/p1"},
		"p2": {BaseEntity: models.BaseEntity{ID: "p2"}, WorkspacePath: "/tmp/p2"},
	}
	profile := models.AgentProfile{
		ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: false,
	}
	return &dispatchBatchStore{
		tasks:    append([]models.Task(nil), tasks...),
		projects: projects,
		profiles: map[string]models.AgentProfile{"ag1": profile},
	}
}

func (s *dispatchBatchStore) Close() error { return nil }

func (s *dispatchBatchStore) GetProject(_ context.Context, id string) (*models.Project, error) {
	if p, ok := s.projects[id]; ok {
		return &p, nil
	}
	return nil, models.ErrProjectNotFound
}

func (s *dispatchBatchStore) GetAgentProfile(_ context.Context, id string) (*models.AgentProfile, error) {
	if p, ok := s.profiles[id]; ok {
		return &p, nil
	}
	return nil, models.ErrAgentProfileNotFound
}

func (s *dispatchBatchStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			t := s.tasks[i]
			return &t, nil
		}
	}
	return nil, models.ErrTaskNotFound
}

func (s *dispatchBatchStore) ClaimNextReadyTasks(_ context.Context, limit int) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var claimed []models.Task
	now := time.Now().UTC()
	for i := range s.tasks {
		if len(claimed) == limit {
			break
		}
		if s.tasks[i].State == models.TaskStateReady {
			s.tasks[i].State = models.TaskStateQueued
			s.tasks[i].UpdatedAt = now
			claimed = append(claimed, s.tasks[i])
		}
	}
	return claimed, nil
}

func (s *dispatchBatchStore) UpdateTaskState(_ context.Context, id string, _ time.Time, next models.TaskState) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].State = next
			return &s.tasks[i], nil
		}
	}
	return nil, models.ErrTaskNotFound
}

func (s *dispatchBatchStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, pid int) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			if s.tasks[i].State != models.TaskStateQueued {
				return nil, models.ErrStateConflict
			}
			now := time.Now().UTC()
			s.tasks[i].State = models.TaskStateRunning
			s.tasks[i].OSProcessID = &pid
			s.tasks[i].LastHeartbeat = &now
			return &s.tasks[i], nil
		}
	}
	return nil, models.ErrTaskNotFound
}

func (s *dispatchBatchStore) UpdateTaskHeartbeat(_ context.Context, id string) error { return nil }

func (s *dispatchBatchStore) UpdateTaskResult(_ context.Context, id string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			if result.Success {
				s.tasks[i].State = models.TaskStateCompleted
			} else {
				s.tasks[i].State = models.TaskStateFailed
			}
			return &s.tasks[i], nil
		}
	}
	return nil, models.ErrTaskNotFound
}

func (s *dispatchBatchStore) IncrementRetryCount(_ context.Context, id string, _ time.Time) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].RetryCount++
			return &s.tasks[i], nil
		}
	}
	return nil, models.ErrTaskNotFound
}

// Stub remaining KanbanStore methods.
func (s *dispatchBatchStore) UpdateTaskDescription(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) AddComment(context.Context, models.Comment) error { return nil }
func (s *dispatchBatchStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ListCommentsSince(context.Context, string, time.Time) ([]models.Comment, error) {
	return nil, nil
}
func (s *dispatchBatchStore) AppendEvent(context.Context, models.Event) error { return nil }
func (s *dispatchBatchStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (s *dispatchBatchStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *dispatchBatchStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *dispatchBatchStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *dispatchBatchStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (s *dispatchBatchStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *dispatchBatchStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *dispatchBatchStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *dispatchBatchStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *dispatchBatchStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }
func (s *dispatchBatchStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return nil, nil
}
func (s *dispatchBatchStore) DeleteAgentProfile(context.Context, string) error { return nil }
func (s *dispatchBatchStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ListSettings(context.Context) ([]models.Setting, error) { return nil, nil }
func (s *dispatchBatchStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (s *dispatchBatchStore) SetSetting(context.Context, string, string) error { return nil }
func (s *dispatchBatchStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}
func (s *dispatchBatchStore) MarkProjectTasksReady(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	p := s.projects["p1"]
	return &p, nil
}
func (s *dispatchBatchStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}
func (s *dispatchBatchStore) ListProjects(context.Context) ([]models.Project, error) { return nil, nil }
func (s *dispatchBatchStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ReconcileStaleTasks(context.Context, []int, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) BlockTaskWithSubtasks(context.Context, string, time.Time, []models.DraftTask) (*models.Task, []models.Task, error) {
	return nil, nil, nil
}
func (s *dispatchBatchStore) ListParentTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (s *dispatchBatchStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
}
func (s *dispatchBatchStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (s *dispatchBatchStore) MarkCommentProcessed(context.Context, string, string) error { return nil }
func (s *dispatchBatchStore) ListTokenUsageEventsSince(_ context.Context, since time.Time) ([]models.TokenUsageEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++
	out := make([]models.TokenUsageEvent, 0, len(s.events))
	for _, ev := range s.events {
		if ev.At.Before(since) {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func TestRequeueUndispatchedClaims_RequeuesQueuedTasks(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	store := newDispatchBatchStore([]models.Task{
		{BaseEntity: models.BaseEntity{ID: "t0", UpdatedAt: now}, ProjectID: "p1", AgentID: "ag1", State: models.TaskStateReady},
		{BaseEntity: models.BaseEntity{ID: "t1", UpdatedAt: now}, ProjectID: "p2", AgentID: "ag1", State: models.TaskStateReady},
	})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{MaxWorkers: 1, Probe: StaticPIDProbe{}})

	claimed, err := store.ClaimNextReadyTasks(context.Background(), 2)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed = %d, want 2", len(claimed))
	}
	// Simulate semaphore failure after the first batch was scheduled: requeue the
	// remaining batch tasks (out-of-order grouping puts t1 in the second batch).
	daemon.requeueUndispatchedClaims(context.Background(), []models.Task{claimed[1]})

	for _, id := range []string{"t0", "t1"} {
		task, err := store.GetTask(context.Background(), id)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", id, err)
		}
		want := models.TaskStateReady
		if id == "t0" {
			want = models.TaskStateQueued
		}
		if task.State != want {
			t.Fatalf("%s state = %s, want %s", id, task.State, want)
		}
	}
}

func TestDispatchGuardNilWorker_CanceledContextRequeues(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	store := newDispatchBatchStore([]models.Task{
		{BaseEntity: models.BaseEntity{ID: "t0", UpdatedAt: now}, ProjectID: "p1", AgentID: "ag1", State: models.TaskStateReady},
		{BaseEntity: models.BaseEntity{ID: "t1", UpdatedAt: now}, ProjectID: "p2", AgentID: "ag1", State: models.TaskStateReady},
	})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{MaxWorkers: 1, Probe: StaticPIDProbe{}})

	claimed, err := store.ClaimNextReadyTasks(context.Background(), 2)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed = %d, want 2", len(claimed))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := daemon.dispatchGuardNilWorker(ctx, claimed); err != nil {
		t.Fatalf("dispatchGuardNilWorker() error = %v, want nil on canceled ctx", err)
	}

	for _, id := range []string{"t0", "t1"} {
		task, err := store.GetTask(context.Background(), id)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", id, err)
		}
		if task.State != models.TaskStateReady {
			t.Fatalf("%s state = %s, want %s after canceled nil-worker requeue", id, task.State, models.TaskStateReady)
		}
	}
}

func TestGroupClaimed_ReordersByProjectBeforeDispatch(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	store := newDispatchBatchStore([]models.Task{
		{BaseEntity: models.BaseEntity{ID: "t0", UpdatedAt: now}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize A", Description: "condense notes", State: models.TaskStateReady},
		{BaseEntity: models.BaseEntity{ID: "t1", UpdatedAt: now}, ProjectID: "p2", AgentID: "ag1", Title: "Summarize B", Description: "condense notes", State: models.TaskStateReady},
		{BaseEntity: models.BaseEntity{ID: "t2", UpdatedAt: now}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize C", Description: "condense notes", State: models.TaskStateReady},
	})
	worker := NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	claimed, err := store.ClaimNextReadyTasks(context.Background(), 3)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	batches := worker.GroupClaimed(context.Background(), claimed)
	if len(batches) != 2 {
		t.Fatalf("batches len = %d, want 2", len(batches))
	}
	if len(batches[0].Tasks) != 2 || batches[0].Tasks[0].ID != "t0" || batches[0].Tasks[1].ID != "t2" {
		t.Fatalf("first batch = %+v, want [t0 t2]", batches[0].Tasks)
	}
	if len(batches[1].Tasks) != 1 || batches[1].Tasks[0].ID != "t1" {
		t.Fatalf("second batch = %+v, want [t1]", batches[1].Tasks)
	}
}

func TestRefreshRollingLedgerFromStore_UsesPersistedEvents(t *testing.T) {
	t.Parallel()
	store := newDispatchBatchStore(nil)
	now := time.Now()
	store.events = []models.TokenUsageEvent{
		{At: now.Add(-30 * time.Minute), Tokens: 35},
		{At: now.Add(-5 * time.Minute), Tokens: 25},
	}
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{MaxWorkers: 1, Probe: StaticPIDProbe{}})
	daemon.rollingLedger = NewRollingTokenLedger(time.Hour, 100)

	daemon.refreshRollingLedgerFromStore(context.Background())

	if got := daemon.rollingLedger.BudgetRemaining(100); got != 40 {
		t.Fatalf("BudgetRemaining = %d, want 40", got)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.queries != 1 {
		t.Fatalf("ListTokenUsageEventsSince queries = %d, want 1", store.queries)
	}
}
