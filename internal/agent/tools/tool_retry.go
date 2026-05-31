package tools

import (
	"context"
	"encoding/json"
	"math/rand"
	"strings"
	"time"

	"agentd/internal/config"
)

// RetryConfig controls the RetryingExecutor backoff behaviour.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// normalize fills zero-valued fields with config package defaults.
func (c RetryConfig) normalize() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = config.DefaultToolRetryMaxAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = config.DefaultToolRetryBaseDelay
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = config.DefaultToolRetryMaxDelay
	}
	return c
}

// RetryDispatchFunc is the signature accepted by RetryingExecutor.
type RetryDispatchFunc func(ctx context.Context) ToolResult

// RetryingExecutor wraps a RetryDispatchFunc and retries tool calls that
// return a retryable result. The model never sees intermediate failures.
type RetryingExecutor struct {
	cfg RetryConfig
}

// NewRetryingExecutor creates an executor with the given config.
func NewRetryingExecutor(cfg RetryConfig) *RetryingExecutor {
	return &RetryingExecutor{cfg: cfg.normalize()}
}

// Execute invokes fn up to cfg.MaxAttempts times when fn returns a
// retryable result. Non-retryable results are returned immediately.
// If all attempts are exhausted the last ToolResult is returned.
func (r *RetryingExecutor) Execute(ctx context.Context, fn RetryDispatchFunc) ToolResult {
	var result ToolResult
	for attempt := 0; attempt < r.cfg.MaxAttempts; attempt++ {
		result = fn(ctx)
		if !shouldRetry(result) {
			return result
		}
		if attempt+1 >= r.cfg.MaxAttempts {
			break
		}
		delay := r.backoff(attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return result
		case <-timer.C:
		}
	}
	result.Retryable = false
	return result
}

// backoffCap returns the exponential cap for the given zero-based attempt:
// min(MaxDelay, BaseDelay * 2^attempt).
func (r *RetryingExecutor) backoffCap(attempt int) time.Duration {
	cap := r.cfg.MaxDelay
	delay := r.cfg.BaseDelay
	for i := 0; i < attempt; i++ {
		if delay >= cap {
			delay = cap
			break
		}
		next := delay * 2
		if next > cap {
			delay = cap
			break
		}
		delay = next
	}
	if delay > cap {
		delay = cap
	}
	return delay
}

// backoff computes exponential delay with full jitter for the given
// zero-based attempt index: uniform in [0, backoffCap(attempt)].
func (r *RetryingExecutor) backoff(attempt int) time.Duration {
	delay := r.backoffCap(attempt)
	if delay <= 0 {
		return 0
	}
	n := int64(delay)
	if n == 1<<63-1 {
		//nolint:gosec // jitter does not need crypto/rand
		return time.Duration(rand.Int63n(n))
	}
	//nolint:gosec // jitter does not need crypto/rand
	return time.Duration(rand.Int63n(n + 1))
}

// shouldRetry reports whether the executor should re-invoke the tool.
func shouldRetry(tr ToolResult) bool {
	if tr.Retryable {
		return true
	}
	return augmentTransientRetryable(tr).Retryable
}

// augmentTransientRetryable upgrades a ToolResult to retryable when its
// content matches known transient error patterns.
func augmentTransientRetryable(tr ToolResult) ToolResult {
	if tr.Retryable || tr.Status == ToolStatusSuccess || tr.Status == ToolStatusVetoed || tr.Status == ToolStatusFatal {
		return tr
	}
	if tr.Status == ToolStatusTimeout {
		tr.Retryable = true
		return tr
	}
	if isTransientErrorContent(tr.Content) {
		tr.Retryable = true
	}
	return tr
}

// isTransientErrorContent returns true when content looks like a transient failure.
func isTransientErrorContent(content string) bool {
	var envelope struct {
		Error  string `json:"error"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return false
	}
	if envelope.Status == "timeout" {
		return true
	}
	lower := strings.ToLower(envelope.Error)
	for _, pattern := range retryablePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// retryablePatterns lists substrings in error messages that indicate
// transient failures worth retrying at the tool level.
var retryablePatterns = []string{
	"timeout",
	"connection refused",
	"connection reset",
	"temporary failure",
	"network unreachable",
	"no such host",
	"i/o timeout",
	"deadline exceeded",
	"service unavailable",
	"too many requests",
	"unexpected eof",
}
