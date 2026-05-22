package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

func TestBatchUserPromptHasThreeSlots(t *testing.T) {
	t.Parallel()
	if n := countBatchSlots(batchUserPrompt(summarizeTasks(3))); n != 3 {
		t.Fatalf("countBatchSlots = %d, want 3; prompt:\n%s", n, batchUserPrompt(summarizeTasks(3)))
	}
}

func TestRunBatchTextGateway_ValidResponse(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
	}
	gw := &batchTrackingGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := summarizeTasks(3)
	req := w.buildBatchRequest(context.Background(), tasks, store.project, store.profile, true)
	for i, m := range req.Messages {
		if m.Role == "user" {
			if got := countBatchSlots(m.Content); got != 3 {
				t.Fatalf("message[%d] slot count = %d, want 3; content:\n%s", i, got, m.Content)
			}
		}
	}
	resp, err := w.runBatchTextGateway(context.Background(), tasks, store.project, store.profile)
	if err != nil {
		t.Fatalf("runBatchTextGateway() error = %v", err)
	}
	if err := validateBatchTextResponse(len(tasks), resp); err != nil {
		t.Fatalf("validateBatchTextResponse() error = %v", err)
	}
}

func TestProcessBatch_ThreeSummarizeTasks_OneLLMCall(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	for _, task := range summarizeTasks(3) {
		store.tasks[task.ID] = task
	}
	gw := &batchTrackingGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})

	w.ProcessBatch(context.Background(), summarizeTasks(3))

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.generateCount != 1 {
		t.Fatalf("generateCount = %d, want 1", gw.generateCount)
	}
	if gw.batchCalls != 1 {
		t.Fatalf("batchCalls = %d, want 1", gw.batchCalls)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.results) != 3 {
		t.Fatalf("committed results = %d, want 3", len(store.results))
	}
}

func TestProcessBatch_MalformedSlot_RerunsSingleTask(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	omit := 1
	gw := &batchTrackingGateway{omitSlot: &omit}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})

	w.ProcessBatch(context.Background(), tasks)

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.batchCalls != 1 {
		t.Fatalf("batchCalls = %d, want 1", gw.batchCalls)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.results["t1"] == nil || !strings.Contains(store.results["t1"].Payload, "summary-0") {
		t.Fatalf("slot 0 payload = %v, want batch summary-0", store.results["t1"])
	}
	if store.results["t3"] == nil || !strings.Contains(store.results["t3"].Payload, "summary-2") {
		t.Fatalf("slot 2 payload = %v, want batch summary-2", store.results["t3"])
	}
	if store.results["t2"] == nil || !strings.Contains(store.results["t2"].Payload, "single fallback") {
		t.Fatalf("slot 1 result payload = %q, want single-task fallback content", store.results["t2"].Payload)
	}
}

func TestProcessBatch_GatewayError_RequeuesAllTasks(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &batchFailingGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})

	w.ProcessBatch(context.Background(), tasks)

	store.mu.Lock()
	defer store.mu.Unlock()
	for _, id := range []string{"t1", "t2", "t3"} {
		task := store.tasks[id]
		if task.State != models.TaskStateReady {
			t.Fatalf("task %s state = %s, want %s after batch gateway error", id, task.State, models.TaskStateReady)
		}
		if task.RetryCount != 1 {
			t.Fatalf("task %s RetryCount = %d, want 1 after batch gateway error", id, task.RetryCount)
		}
		if store.results[id] != nil {
			t.Fatalf("task %s should not have a terminal result on first gateway error", id)
		}
	}
}

func TestProcessBatch_LegacyThreeTasks_OneLLMCallThreeSandbox(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: false,
		},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &batchTrackingGateway{}
	sb := &batchCountingSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		Batching: config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
	})

	w.ProcessBatch(context.Background(), tasks)

	gw.mu.Lock()
	if gw.generateCount != 1 {
		t.Errorf("generateCount = %d, want 1", gw.generateCount)
	}
	gw.mu.Unlock()
	sb.mu.Lock()
	if sb.calls != 3 {
		t.Errorf("sandbox calls = %d, want 3", sb.calls)
	}
	sb.mu.Unlock()
}
