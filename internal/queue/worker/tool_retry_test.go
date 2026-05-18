package worker

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agentd/internal/config"
)

func TestRetryingExecutor_NonRetryableReturnsImmediately(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    50 * time.Millisecond,
	})

	var calls int32
	result := executor.Execute(context.Background(), func(_ context.Context) ToolResult {
		atomic.AddInt32(&calls, 1)
		return SuccessResult("c1", `{"ok":true}`, 0)
	})

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if result.Content != `{"ok":true}` {
		t.Fatalf("unexpected content: %s", result.Content)
	}
	if result.Retryable {
		t.Fatal("expected non-retryable result")
	}
}

func TestRetryingExecutor_RetryableExhaustsAttempts(t *testing.T) {
	const maxAttempts = 3
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: maxAttempts,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	})

	var calls int32
	result := executor.Execute(context.Background(), func(_ context.Context) ToolResult {
		atomic.AddInt32(&calls, 1)
		return augmentTransientRetryable(NonRetryableErrorResult("c1", `{"error":"connection refused"}`, "", 0))
	})

	if atomic.LoadInt32(&calls) != maxAttempts {
		t.Fatalf("expected %d calls, got %d", maxAttempts, calls)
	}
	if !strings.Contains(result.Content, "connection refused") {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

func TestRetryingExecutor_SuccessfulRetry(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	})

	var calls int32
	result := executor.Execute(context.Background(), func(_ context.Context) ToolResult {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return TimeoutResult("c1", 100)
		}
		return SuccessResult("c1", `{"ok":true}`, 0)
	})

	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if result.Content != `{"ok":true}` {
		t.Fatalf("unexpected content: %s", result.Content)
	}
	if result.Retryable {
		t.Fatal("expected non-retryable success result")
	}
}

func TestRetryingExecutor_BackoffIncreases(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: 4,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    1 * time.Second,
	})

	var timestamps []time.Time
	executor.Execute(context.Background(), func(_ context.Context) ToolResult {
		timestamps = append(timestamps, time.Now())
		return TimeoutResult("c1", 100)
	})

	if len(timestamps) != 4 {
		t.Fatalf("expected 4 timestamps, got %d", len(timestamps))
	}

	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap <= 0 {
			t.Fatalf("expected a retry delay between attempt %d and %d, got %v", i-1, i, gap)
		}
	}
}

func TestRetryingExecutor_ContextCancelled(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	result := executor.Execute(ctx, func(_ context.Context) ToolResult {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			cancel()
		}
		return TimeoutResult("c1", 100)
	})

	got := atomic.LoadInt32(&calls)
	if got >= 5 {
		t.Fatalf("expected fewer than 5 calls due to cancellation, got %d", got)
	}
	if result.Status != ToolStatusTimeout {
		t.Fatalf("unexpected status: %s", result.Status)
	}
}

func TestRetryingExecutor_DefaultConfig(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{})

	if executor.cfg.MaxAttempts != config.DefaultToolRetryMaxAttempts {
		t.Fatalf("expected MaxAttempts=%d, got %d", config.DefaultToolRetryMaxAttempts, executor.cfg.MaxAttempts)
	}
	if executor.cfg.BaseDelay != config.DefaultToolRetryBaseDelay {
		t.Fatalf("expected BaseDelay=%v, got %v", config.DefaultToolRetryBaseDelay, executor.cfg.BaseDelay)
	}
	if executor.cfg.MaxDelay != config.DefaultToolRetryMaxDelay {
		t.Fatalf("expected MaxDelay=%v, got %v", config.DefaultToolRetryMaxDelay, executor.cfg.MaxDelay)
	}
}

func TestRetryingExecutor_SingleAttemptNoRetry(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: 1,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	})

	var calls int32
	result := executor.Execute(context.Background(), func(_ context.Context) ToolResult {
		atomic.AddInt32(&calls, 1)
		return TimeoutResult("c1", 100)
	})

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call with MaxAttempts=1, got %d", calls)
	}
	if !result.Retryable {
		t.Fatal("expected retryable result")
	}
}

func TestRetryingExecutor_BackoffHighAttemptNoPanic(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{
		MaxAttempts: 100,
		BaseDelay:   time.Second,
		MaxDelay:    5 * time.Second,
	})
	for attempt := 0; attempt < 50; attempt++ {
		_ = executor.backoff(attempt)
	}
}

func TestAugmentTransientRetryable_Timeout(t *testing.T) {
	tr := augmentTransientRetryable(TimeoutResult("c1", 60000))
	if !tr.Retryable {
		t.Fatal("expected timeout result to be retryable")
	}
}

func TestAugmentTransientRetryable_ConnectionRefused(t *testing.T) {
	tr := augmentTransientRetryable(NonRetryableErrorResult("c1", `{"error":"execution failed: connection refused"}`, "", 0))
	if !tr.Retryable {
		t.Fatal("expected connection refused to be retryable")
	}
}

func TestAugmentTransientRetryable_NonRetryableError(t *testing.T) {
	tr := augmentTransientRetryable(NonRetryableErrorResult("c1", `{"error":"command failed with exit code 1: ls: no such file"}`, "", 0))
	if tr.Retryable {
		t.Fatal("expected non-retryable result for generic command failure")
	}
}

func TestAugmentTransientRetryable_Success(t *testing.T) {
	tr := augmentTransientRetryable(SuccessResult("c1", `{"ok":true}`, 0))
	if tr.Retryable {
		t.Fatal("expected success result to be non-retryable")
	}
}

func TestIsTransientErrorContent_Patterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "timeout status", input: `{"status":"timeout","error":"timed out"}`, want: true},
		{name: "connection reset", input: `{"error":"connection reset by peer"}`, want: true},
		{name: "temporary failure", input: `{"error":"temporary failure in name resolution"}`, want: true},
		{name: "network unreachable", input: `{"error":"network unreachable"}`, want: true},
		{name: "i/o timeout", input: `{"error":"i/o timeout"}`, want: true},
		{name: "service unavailable", input: `{"error":"503 service unavailable"}`, want: true},
		{name: "too many requests", input: `{"error":"429 too many requests"}`, want: true},
		{name: "eof", input: `{"error":"unexpected eof"}`, want: true},
		{name: "deadline exceeded", input: `{"error":"context deadline exceeded"}`, want: true},
		{name: "no such host", input: `{"error":"dial tcp: lookup foo: no such host"}`, want: true},
		{name: "permission denied", input: `{"error":"permission denied"}`, want: false},
		{name: "file not found", input: `{"error":"no such file or directory"}`, want: false},
		{name: "syntax error", input: `{"error":"syntax error near unexpected token"}`, want: false},
		{name: "empty error", input: `{"error":""}`, want: false},
		{name: "success", input: `{"Success":true}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientErrorContent(tt.input)
			if got != tt.want {
				t.Fatalf("isTransientErrorContent(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
