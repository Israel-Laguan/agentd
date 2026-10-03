package worker

import (
	"context"
	"sync"
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

type eventLog struct {
	mu    sync.Mutex
	types []string
}

func (l *eventLog) Emit(_ context.Context, e models.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.types = append(l.types, string(e.Type))
	return nil
}

func (l *eventLog) count(kind string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, got := range l.types {
		if got == kind {
			n++
		}
	}
	return n
}

func trippedOpenAIBreakers(clock *time.Time) *safety.ProviderBreakers {
	pb := safety.NewProviderBreakers()
	pb.Get("openai").SetClockForTest(func() time.Time { return *clock })
	for i := 0; i < 3; i++ {
		pb.Get("openai").RecordError(models.ErrLLMQuotaExceeded)
	}
	return pb
}

func batchBreakerStore(task models.Task) *batchTestStore {
	return &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
		tasks:   map[string]models.Task{task.ID: task},
		results: make(map[string]*models.TaskResult),
	}
}

// B-013: waiting on a probe is not a retry, and must not leave a RETRY event per tick.
func TestProcessRunningTask_ProbeInFlightEmitsNoRetry(t *testing.T) {
	t.Parallel()
	task := summarizeTasks(1)[0]
	store := batchBreakerStore(task)
	clock := time.Now()
	pb := trippedOpenAIBreakers(&clock)
	clock = clock.Add(6 * time.Minute)
	if got := pb.Get("openai").Admit(); got != safety.AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want the probe slot", got)
	}
	log := &eventLog{}
	w := NewWorker(store, &batchTrackingGateway{}, nil, nil, log, WorkerOptions{ProviderBreakers: pb})

	w.processRunningTask(context.Background(), task, store.project, store.profile)

	if n := log.count("RETRY"); n != 0 {
		t.Fatalf("RETRY events = %d, want 0", n)
	}
	if got := store.tasks[task.ID].RetryCount; got != 0 {
		t.Fatalf("RetryCount = %d, want 0", got)
	}
}

// B-013: only a probe in flight waits; a breaker still inside its open timeout hands off.
func TestProcessRunningTask_OpenBreakerStillHandsOff(t *testing.T) {
	t.Parallel()
	task := summarizeTasks(1)[0]
	store := batchBreakerStore(task)
	clock := time.Now()
	pb := trippedOpenAIBreakers(&clock)
	gw := &batchTrackingGateway{}
	log := &eventLog{}
	w := NewWorker(store, gw, nil, nil, log, WorkerOptions{ProviderBreakers: pb})

	w.processRunningTask(context.Background(), task, store.project, store.profile)

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.generateCount != 0 {
		t.Fatalf("gateway called %d times while the breaker is OPEN", gw.generateCount)
	}
	if got := store.tasks[task.ID].State; got == models.TaskStateReady {
		t.Fatalf("state = %s, want a handoff or failure, not a requeue, while OPEN", got)
	}
}
