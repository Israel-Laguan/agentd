package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// flakySandbox fails the first failCount Execute calls with a transient error,
// then succeeds.
type flakySandbox struct {
	mu        sync.Mutex
	calls     int
	failCount int
}

func (f *flakySandbox) Execute(ctx context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if n <= f.failCount {
		return sandbox.Result{}, errors.New("connection refused")
	}
	select {
	case <-ctx.Done():
		return sandbox.Result{}, ctx.Err()
	default:
		return sandbox.Result{Stdout: "ok\n", Success: true}, nil
	}
}

func (f *flakySandbox) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// timeoutThenOKSandbox blocks until cancelled on the first call (simulating
// a tool that exceeds its per-attempt timeout), then succeeds immediately.
type timeoutThenOKSandbox struct {
	mu    sync.Mutex
	calls int
}

func (s *timeoutThenOKSandbox) Execute(ctx context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == 1 {
		select {
		case <-ctx.Done():
			return sandbox.Result{}, ctx.Err()
		case <-time.After(5 * time.Second):
			return sandbox.Result{Stdout: "late\n", Success: true}, nil
		}
	}
	return sandbox.Result{Stdout: "ok\n", Success: true}, nil
}

func (s *timeoutThenOKSandbox) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func retryTestWorker(t *testing.T, sb sandbox.Executor, allowTools ...string) *Worker {
	t.Helper()
	tools := make(map[string]struct{}, len(allowTools))
	for _, name := range allowTools {
		tools[name] = struct{}{}
	}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	return &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{"bash": 30 * time.Second},
		},
		toolRetries: config.ToolRetriesConfig{
			MaxAttempts: 3,
			BaseDelay:   1 * time.Millisecond,
			MaxDelay:    5 * time.Millisecond,
			Tools:       tools,
		},
		toolRetrier: NewRetryingExecutor(RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   1 * time.Millisecond,
			MaxDelay:    5 * time.Millisecond,
		}),
		hooks: NewHookChain(),
	}
}

func TestDispatchToolWithHooks_RetryHidesIntermediateFailure(t *testing.T) {
	t.Parallel()
	sb := &flakySandbox{failCount: 1}
	w := retryTestWorker(t, sb, "bash")

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(), call, nil, w.toolExecutor, nil, nil,
	)

	if suspended {
		t.Fatal("expected suspend=false")
	}
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("status = %s, want success", tr.Status)
	}
	if !strings.Contains(tr.Content, "ok") {
		t.Fatalf("expected success output, got %q", tr.Content)
	}
	if sb.callCount() != 2 {
		t.Fatalf("expected 2 sandbox calls, got %d", sb.callCount())
	}
}

func TestDispatchToolWithHooks_SuspendNotRetried(t *testing.T) {
	t.Parallel()
	sb := &flakySandbox{failCount: 10}
	w := retryTestWorker(t, sb, "bash")
	taskHooks := NewHookChain()
	taskHooks.RegisterPre(PreHook{
		Name:   "suspend-gate",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{Veto: true, Suspend: true, Result: "paused"}, nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(), call, nil, w.toolExecutor, taskHooks, nil,
	)

	if !suspended {
		t.Fatal("expected suspended=true")
	}
	if sb.callCount() != 0 {
		t.Fatalf("expected no sandbox calls on suspend, got %d", sb.callCount())
	}
	if tr.Content != "paused" {
		t.Fatalf("unexpected content: %q", tr.Content)
	}
}

func TestDispatchToolWithHooks_RetryExhaustion(t *testing.T) {
	t.Parallel()
	sb := &flakySandbox{failCount: 10}
	w := retryTestWorker(t, sb, "bash")

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(), call, nil, w.toolExecutor, nil, nil,
	)

	if suspended {
		t.Fatal("expected suspend=false")
	}
	if sb.callCount() != 3 {
		t.Fatalf("expected 3 sandbox calls on exhaustion, got %d", sb.callCount())
	}
	if !strings.Contains(tr.Content, "connection refused") {
		t.Fatalf("expected last attempt error, got %q", tr.Content)
	}
}

func TestDispatchToolWithHooks_RetryAuditsOnce(t *testing.T) {
	t.Parallel()
	sb := &flakySandbox{failCount: 1}
	sink := &mockEventSink{}
	w := retryTestWorker(t, sb, "bash")
	w.hooks.RegisterPost(AuditHook(sink, nil))

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(), call, nil, w.toolExecutor, nil, nil,
	)

	if suspended {
		t.Fatal("expected suspend=false")
	}
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("status = %s, want success", tr.Status)
	}
	if sb.callCount() != 2 {
		t.Fatalf("expected 2 sandbox calls, got %d", sb.callCount())
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events (one logical dispatch), got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeToolCall {
		t.Fatalf("first event should be TOOL_CALL, got %q", sink.events[0].Type)
	}
	if sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("second event should be TOOL_RESULT, got %q", sink.events[1].Type)
	}
}

func TestDispatchToolWithHooks_TimeoutRetry(t *testing.T) {
	t.Parallel()
	sb := &timeoutThenOKSandbox{}
	w := retryTestWorker(t, sb, "bash")
	w.toolTimeouts = config.ToolTimeoutsConfig{
		Defaults: map[string]time.Duration{"bash": 50 * time.Millisecond},
	}

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 10"}`},
	}

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(), call, nil, w.toolExecutor, nil, nil,
	)

	if suspended {
		t.Fatal("expected suspend=false")
	}
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("status = %s, want success after timeout retry", tr.Status)
	}
	if !strings.Contains(tr.Content, "ok") {
		t.Fatalf("expected success output, got %q", tr.Content)
	}
	if sb.callCount() != 2 {
		t.Fatalf("expected 2 sandbox calls (timeout then success), got %d", sb.callCount())
	}
}

func TestDispatchToolWithHooks_NonAllowlistedNoRetry(t *testing.T) {
	t.Parallel()
	sb := &flakySandbox{failCount: 1}
	w := retryTestWorker(t, sb, "read") // bash not allowlisted

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	_, _ = w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(), call, nil, w.toolExecutor, nil, nil,
	)

	if sb.callCount() != 1 {
		t.Fatalf("expected 1 sandbox call without retry, got %d", sb.callCount())
	}
}
