package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestWorkerCompletesSuccessfulTask(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"echo ok"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})
	worker.Process(context.Background(), store.task)
	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v", store.result)
	}
}

func TestWorkerHeartbeatUpdatesWhileRunning(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"sleep 1"}`}
	sb := &fakeSandbox{
		result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"},
		delay:  25 * time.Millisecond,
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{HeartbeatInterval: 5 * time.Millisecond})

	worker.Process(context.Background(), store.task)

	if store.heartbeats == 0 {
		t.Fatal("worker did not update task heartbeat during sandbox execution")
	}
}

func TestWorkerRetriesThenEvicts(t *testing.T) {
	store := newWorkerStore()
	store.task.RetryCount = 2
	gw := &fakeGateway{content: `{"command":"false"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: false, ExitCode: 1, Stderr: "boom"}}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), sink, WorkerOptions{MaxRetries: 3})
	worker.Process(context.Background(), store.task)
	if store.task.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state=%s, want FAILED_REQUIRES_HUMAN", store.task.State)
	}
	if !sink.hasEvent("POISON_PILL_HANDOFF") {
		t.Fatalf("expected POISON_PILL_HANDOFF event")
	}
}

func TestWorkerBreakerErrorRequeuesWithoutRetry(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{err: models.ErrLLMUnreachable}
	breaker := NewCircuitBreaker()
	worker := NewWorker(store, gw, &fakeSandbox{}, breaker, nil, WorkerOptions{})
	worker.Process(context.Background(), store.task)
	if store.task.RetryCount != 0 || store.task.State != models.TaskStateReady {
		t.Fatalf("state=%s retries=%d", store.task.State, store.task.RetryCount)
	}
}

func TestWorkerBreakerErrorCreatesHumanTaskWhenBreakerOpens(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{err: models.ErrLLMUnreachable}
	breaker := NewCircuitBreaker()
	sink := &recordingSink{}
	worker := NewWorker(store, gw, &fakeSandbox{}, breaker, sink, WorkerOptions{})

	worker.Process(context.Background(), store.task)
	worker.Process(context.Background(), store.task)
	worker.Process(context.Background(), store.task)

	if store.task.State != models.TaskStateBlocked {
		t.Fatalf("state=%s, want BLOCKED", store.task.State)
	}
	if len(store.drafts) != 1 || store.drafts[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("drafts = %#v", store.drafts)
	}
	if !strings.Contains(store.drafts[0].Description, "All configured AI providers failed") {
		t.Fatalf("description = %q", store.drafts[0].Description)
	}
	if !sink.hasEvent("PROVIDER_EXHAUSTED_HANDOFF") {
		t.Fatalf("events = %#v, want PROVIDER_EXHAUSTED_HANDOFF", sink.events)
	}
}

func TestWorkerBreaksDownTooComplexTask(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"too_complex":true,"subtasks":[{"title":"First","description":"Do first"},{"title":"Second","description":"Do second"}]}`}
	sb := &fakeSandbox{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

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
	if len(sb.commands) != 0 {
		t.Fatalf("commands = %#v, want none", sb.commands)
	}
}

func TestWorkerPayloadUsesSandboxEnvAllowlist(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "super-secret")
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("LANG", "C")

	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"env"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{
		SandboxWallTimeout:  2 * time.Minute,
		SandboxEnvAllowlist: []string{"PATH", "LANG"},
		SandboxExtraEnv:     []string{"CI=true"},
	})

	worker.Process(context.Background(), store.task)

	if len(sb.payloads) == 0 {
		t.Fatalf("sandbox payloads = 0, want > 0")
	}
	payload := sb.payloads[0]
	if payload.WallTimeout != 2*time.Minute {
		t.Fatalf("WallTimeout = %s, want 2m", payload.WallTimeout)
	}
	envBlob := strings.Join(payload.EnvVars, "\n")
	if !strings.Contains(envBlob, "PATH=/usr/bin") {
		t.Fatalf("env missing PATH: %q", envBlob)
	}
	if !strings.Contains(envBlob, "LANG=C") {
		t.Fatalf("env missing LANG: %q", envBlob)
	}
	if strings.Contains(envBlob, "OPENAI_API_KEY=super-secret") {
		t.Fatalf("env leaked OPENAI_API_KEY: %q", envBlob)
	}
	if !strings.Contains(envBlob, "CI=true") {
		t.Fatalf("env missing extra entry CI=true: %q", envBlob)
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

func TestWorkerPayloadEnvironmentDoesNotDependOnProcessEnvOrder(t *testing.T) {
	t.Setenv("PATH", "/bin")
	t.Setenv("HOME", "/tmp/home")
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"echo ok"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{
		SandboxEnvAllowlist: []string{"HOME", "PATH"},
	})
	worker.Process(context.Background(), store.task)
	if len(sb.payloads) == 0 {
		t.Fatal("expected payload")
	}
	got := strings.Join(sb.payloads[0].EnvVars, "\n")
	if !strings.Contains(got, "PATH=") || !strings.Contains(got, "HOME=") {
		t.Fatalf("payload env = %q", got)
	}
}

func TestWorkerAgenticModeExecutesToolAndContinuesLoop(t *testing.T) {
	store := newWorkerStore()
	store.profile.AgenticMode = true
	gw := &fakeGateway{
		content:       "Task completed successfully",
		toolCalls:     []gateway.ToolCall{{ID: "call_1", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`}}},
		nextContent:   "Task completed successfully",
		nextToolCalls: nil,
	}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "hello"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v, want success", store.result)
	}
	if len(sb.commands) != 1 || sb.commands[0] != "echo hello" {
		t.Fatalf("commands = %#v, want [echo hello]", sb.commands)
	}
	if len(gw.requests) != 2 {
		t.Fatalf("gateway requests = %d, want 2", len(gw.requests))
	}
}

func TestWorkerAgenticModeTerminatesLoopOnTextResponse(t *testing.T) {
	store := newWorkerStore()
	store.profile.AgenticMode = true
	gw := &fakeGateway{
		content: "Final response text",
	}
	sb := &fakeSandbox{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v, want success", store.result)
	}
	if !strings.Contains(store.result.Payload, "Final response text") {
		t.Fatalf("payload = %v, want Final response text", store.result.Payload)
	}
	if len(gw.requests) != 1 {
		t.Fatalf("gateway requests = %d, want 1", len(gw.requests))
	}
}

func TestWorkerAgenticModeHandlesToolExecutionError(t *testing.T) {
	store := newWorkerStore()
	store.profile.AgenticMode = true
	gw := &fakeGateway{
		content:       "Task completed",
		toolCalls:     []gateway.ToolCall{{ID: "call_1", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"false"}`}}},
		nextContent:   "Task completed",
		nextToolCalls: nil,
	}
	sb := &fakeSandbox{result: sandbox.Result{Success: false, ExitCode: 1, Stderr: "command failed"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v, want success", store.result)
	}
}

func TestWorkerLegacyPathWhenAgenticModeFalse(t *testing.T) {
	store := newWorkerStore()
	store.profile.AgenticMode = false
	gw := &fakeGateway{content: `{"command":"echo legacy"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "legacy"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v", store.result)
	}
	if len(sb.commands) != 1 || sb.commands[0] != "echo legacy" {
		t.Fatalf("commands = %#v", sb.commands)
	}
}
