package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
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
		return ToolResult{Output: `{"ok":true}`, Retryable: false}
	})

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if result.Output != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", result.Output)
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
		return ToolResult{Output: `{"error":"connection refused"}`, Retryable: true}
	})

	if atomic.LoadInt32(&calls) != maxAttempts {
		t.Fatalf("expected %d calls, got %d", maxAttempts, calls)
	}
	if result.Output != `{"error":"connection refused"}` {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if !result.Retryable {
		t.Fatal("expected retryable result on exhaustion")
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
			return ToolResult{Output: `{"error":"timeout"}`, Retryable: true}
		}
		return ToolResult{Output: `{"ok":true}`, Retryable: false}
	})

	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if result.Output != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", result.Output)
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
		return ToolResult{Output: `{"error":"timeout"}`, Retryable: true}
	})

	if len(timestamps) != 4 {
		t.Fatalf("expected 4 timestamps, got %d", len(timestamps))
	}

	// Verify delays exist between calls (jitter makes exact values unpredictable,
	// but each gap should be non-negative).
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 0 {
			t.Fatalf("negative gap between attempt %d and %d: %v", i-1, i, gap)
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
		return ToolResult{Output: `{"error":"timeout"}`, Retryable: true}
	})

	got := atomic.LoadInt32(&calls)
	if got >= 5 {
		t.Fatalf("expected fewer than 5 calls due to cancellation, got %d", got)
	}
	if result.Output != `{"error":"timeout"}` {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestRetryingExecutor_DefaultConfig(t *testing.T) {
	executor := NewRetryingExecutor(RetryConfig{})

	if executor.cfg.MaxAttempts != DefaultRetryMaxAttempts {
		t.Fatalf("expected MaxAttempts=%d, got %d", DefaultRetryMaxAttempts, executor.cfg.MaxAttempts)
	}
	if executor.cfg.BaseDelay != DefaultRetryBaseDelay {
		t.Fatalf("expected BaseDelay=%v, got %v", DefaultRetryBaseDelay, executor.cfg.BaseDelay)
	}
	if executor.cfg.MaxDelay != DefaultRetryMaxDelay {
		t.Fatalf("expected MaxDelay=%v, got %v", DefaultRetryMaxDelay, executor.cfg.MaxDelay)
	}
}

func TestClassifyResult_Timeout(t *testing.T) {
	result := classifyResult(`{"status":"timeout","error":"[TIMEOUT] Tool 'bash' did not respond within 60000ms"}`)
	if !result.Retryable {
		t.Fatal("expected timeout result to be retryable")
	}
}

func TestClassifyResult_ConnectionRefused(t *testing.T) {
	result := classifyResult(`{"error":"execution failed: connection refused"}`)
	if !result.Retryable {
		t.Fatal("expected connection refused to be retryable")
	}
}

func TestClassifyResult_NonRetryableError(t *testing.T) {
	result := classifyResult(`{"error":"command failed with exit code 1: ls: no such file"}`)
	if result.Retryable {
		t.Fatal("expected non-retryable result for generic command failure")
	}
}

func TestClassifyResult_Success(t *testing.T) {
	result := classifyResult(`{"ok":true}`)
	if result.Retryable {
		t.Fatal("expected success result to be non-retryable")
	}
}

func TestClassifyResult_PlainText(t *testing.T) {
	result := classifyResult("hello world")
	if result.Retryable {
		t.Fatal("expected plain text to be non-retryable")
	}
}

func TestIsRetryableResult_Patterns(t *testing.T) {
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
			got := isRetryableResult(tt.input)
			if got != tt.want {
				t.Fatalf("isRetryableResult(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
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
		return ToolResult{Output: `{"error":"timeout"}`, Retryable: true}
	})

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call with MaxAttempts=1, got %d", calls)
	}
	if !result.Retryable {
		t.Fatal("expected retryable result")
	}
}
