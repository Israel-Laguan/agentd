package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/config"
	"agentd/internal/models"
)

func TestWorkerCompletesSuccessfulTask(t *testing.T) {
	store := newACPWorkerStore(t)
	scriptTask(store, acpReport("done"))
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, nil, WorkerOptions{})
	worker.Process(context.Background(), store.task)
	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v", store.result)
	}
}

func TestWorkerHeartbeatUpdatesWhileRunning(t *testing.T) {
	store := newACPWorkerStore(t)
	// The agent stays in its turn long enough for the heartbeat to tick.
	scriptTask(store, fakeagent.Step{SleepMS: 150})
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, nil, WorkerOptions{HeartbeatInterval: 5 * time.Millisecond})

	worker.Process(context.Background(), store.task)

	if store.heartbeats == 0 {
		t.Fatal("worker did not update task heartbeat while the agent ran")
	}
}

func TestWorkerRetriesThenEvicts(t *testing.T) {
	store := newACPWorkerStore(t)
	store.task.RetryCount = 2
	scriptTask(store, fakeagent.Step{Error: "tool crashed"})
	sink := &recordingSink{}
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, sink, WorkerOptions{MaxRetries: 3})
	worker.Process(context.Background(), store.task)
	if store.task.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state=%s, want FAILED_REQUIRES_HUMAN", store.task.State)
	}
	if !sink.hasEvent("POISON_PILL_HANDOFF") {
		t.Fatalf("expected POISON_PILL_HANDOFF event")
	}
}

// The healing tuner's split step runs when the agent's attempt fails: the
// coordinator asks its own L1 split stage for subtasks and blocks the task on
// them, without running the agent again.
func TestWorkerBreaksDownTooComplexTask(t *testing.T) {
	store := newACPWorkerStore(t)
	scriptTask(store, fakeagent.Step{Error: "too complex for one turn"})
	gw := &fakeGateway{
		content: `{"subtasks":[{"title":"First","description":"Do first"},{"title":"Second","description":"Do second"}]}`,
	}
	tuner := NewParameterTuner(config.HealingConfig{Enabled: true, Steps: []string{HealingStepSplitTask}})
	worker := newACPWorker(t, store, gw, &fakeSandbox{}, nil, WorkerOptions{Tuner: tuner})

	worker.Process(context.Background(), store.task)

	if store.appends != 1 {
		t.Fatalf("appends = %d, want 1", store.appends)
	}
	if store.task.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED", store.task.State)
	}
	if len(store.drafts) != 2 || store.drafts[0].Title != "First" || store.drafts[1].Title != "Second" {
		t.Fatalf("drafts = %#v", store.drafts)
	}
}

func TestBuildSandboxEnvWithNilAllowlistDoesNotInherit(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "super-secret")
	env := BuildSandboxEnv(nil, []string{"CI=true"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "OPENAI_API_KEY=super-secret") {
		t.Fatalf("env leaked OPENAI_API_KEY: %q", joined)
	}
	if !strings.Contains(joined, "CI=true") {
		t.Fatalf("env missing CI=true: %q", joined)
	}
	if len(env) != 1 {
		t.Fatalf("env len = %d, want 1", len(env))
	}
}

// An attempt the agent fails is retried, not recorded: a failed turn is the
// agent's failure, so the coordinator counts a retry and leaves the task READY.
func TestWorkerAgenticModeHandlesToolExecutionError(t *testing.T) {
	store := newACPWorkerStore(t)
	scriptTask(store, fakeagent.Step{Error: "tool crashed"})
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.result != nil || store.task.State != models.TaskStateReady || store.task.RetryCount != 1 {
		t.Fatalf("result = %#v state = %s retries = %d, want no result and a retry after a failed turn", store.result, store.task.State, store.task.RetryCount)
	}
}

// TestWorkerQuotaExhaustedCreatesImmediateHandoff verifies that a single quota
// failure (the agent reporting a 429, which the ACP driver maps to
// ErrLLMQuotaExceeded) creates a PROVIDER_EXHAUSTED_HANDOFF immediately without
// requeuing the task or incrementing the retry counter.
func TestWorkerQuotaExhaustedCreatesImmediateHandoff(t *testing.T) {
	store := newACPWorkerStore(t)
	scriptTask(store, fakeagent.Step{Error: "429 Too Many Requests"})
	sink := &recordingSink{}
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, sink, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.task.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED", store.task.State)
	}
	if len(store.drafts) != 1 || store.drafts[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("drafts = %#v", store.drafts)
	}
	if !strings.Contains(store.drafts[0].Description, "quota exhausted") {
		t.Fatalf("description = %q, want quota message", store.drafts[0].Description)
	}
	if !sink.hasEvent("PROVIDER_EXHAUSTED_HANDOFF") {
		t.Fatalf("events = %#v, want PROVIDER_EXHAUSTED_HANDOFF", sink.events)
	}
	// Must not requeue: RetryCount stays at 0.
	if store.task.RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0 (no requeue on quota)", store.task.RetryCount)
	}
}

// TestWorkerPerProviderBreakerIsolation verifies that a quota error recorded
// against "gemini" opens only the gemini breaker and leaves the "horde" breaker
// unaffected.
func TestWorkerPerProviderBreakerIsolation(t *testing.T) {
	storeGemini := newACPWorkerStore(t)
	storeGemini.profile.Provider = "gemini"
	scriptTask(storeGemini, fakeagent.Step{Error: "429 Too Many Requests"})
	pb := NewProviderBreakers()
	workerGemini := newACPWorker(t, storeGemini, &fakeGateway{}, &fakeSandbox{}, nil,
		WorkerOptions{ProviderBreakers: pb})

	workerGemini.Process(context.Background(), storeGemini.task)

	geminiBreaker := pb.Get("gemini")
	if geminiBreaker.FailureCount() == 0 {
		t.Fatal("gemini breaker should have recorded failure")
	}

	hordeBreaker := pb.Get("horde")
	if hordeBreaker.State() != BreakerClosed {
		t.Fatalf("horde breaker state = %s, want CLOSED", hordeBreaker.State())
	}
}
