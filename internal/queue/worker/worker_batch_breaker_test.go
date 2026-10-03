package worker

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
)

// B-012: the batch path gates on the provider breaker too, so it must admit a
// probe once the open timeout has passed instead of handing the task off forever.
func TestProcessRunningTask_ProviderBreakerAdmitsProbeAfterTimeout(t *testing.T) {
	t.Parallel()
	task := summarizeTasks(1)[0]
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
		tasks:   map[string]models.Task{task.ID: task},
		results: make(map[string]*models.TaskResult),
	}
	pb := safety.NewProviderBreakers()
	clock := time.Now()
	pb.Get("openai").SetClockForTest(func() time.Time { return clock })
	for i := 0; i < 3; i++ {
		pb.Get("openai").RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(6 * time.Minute)
	gw := &batchTrackingGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{ProviderBreakers: pb})

	w.processRunningTask(context.Background(), task, store.project, store.profile)

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.generateCount == 0 {
		t.Fatal("gateway was never called: the batch path still hands the task off after the timeout")
	}
}

// B-013: on the batch path a sibling of an in-flight probe goes back to READY
// instead of being handed off.
func TestProcessRunningTask_ProviderProbeInFlightRequeues(t *testing.T) {
	t.Parallel()
	task := summarizeTasks(1)[0]
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
		tasks:   map[string]models.Task{task.ID: task},
		results: make(map[string]*models.TaskResult),
	}
	pb := safety.NewProviderBreakers()
	clock := time.Now()
	pb.Get("openai").SetClockForTest(func() time.Time { return clock })
	for i := 0; i < 3; i++ {
		pb.Get("openai").RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(6 * time.Minute)
	if got := pb.Get("openai").Admit(); got != safety.AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want the probe slot", got)
	}
	gw := &batchTrackingGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{ProviderBreakers: pb})

	w.processRunningTask(context.Background(), task, store.project, store.profile)

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.generateCount != 0 {
		t.Fatalf("gateway called %d times while another task holds the probe slot", gw.generateCount)
	}
	if got := store.tasks[task.ID].State; got != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", got)
	}
}
