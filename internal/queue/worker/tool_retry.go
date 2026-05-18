package worker

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"strings"
	"time"
)

// DefaultRetryMaxAttempts is the default number of attempts for tool-level retries.
const DefaultRetryMaxAttempts = 3

// DefaultRetryBaseDelay is the base delay between retry attempts.
const DefaultRetryBaseDelay = 200 * time.Millisecond

// DefaultRetryMaxDelay is the ceiling for exponential backoff.
const DefaultRetryMaxDelay = 5 * time.Second

// ToolResult pairs a tool's output string with a retryable flag.
// When Retryable is true the RetryingExecutor may re-invoke the tool
// call transparently before surfacing the failure to the agentic loop.
type ToolResult struct {
	Output    string
	Retryable bool
}

// RetryConfig controls the RetryingExecutor backoff behaviour.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// normalize fills zero-valued fields with defaults.
func (c RetryConfig) normalize() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = DefaultRetryMaxAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = DefaultRetryBaseDelay
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = DefaultRetryMaxDelay
	}
	return c
}

// DispatchFunc is the signature accepted by RetryingExecutor.
// It mirrors the return shape needed to classify retryable results.
type DispatchFunc func(ctx context.Context) ToolResult

// RetryingExecutor wraps a DispatchFunc and retries tool calls that
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
func (r *RetryingExecutor) Execute(ctx context.Context, fn DispatchFunc) ToolResult {
	var result ToolResult
	for attempt := 0; attempt < r.cfg.MaxAttempts; attempt++ {
		result = fn(ctx)
		if !result.Retryable {
			return result
		}
		if attempt+1 >= r.cfg.MaxAttempts {
			break
		}
		delay := r.backoff(attempt)
		select {
		case <-ctx.Done():
			return result
		case <-time.After(delay):
		}
	}
	return result
}

// backoff computes exponential delay with full jitter for the given
// zero-based attempt index: min(MaxDelay, BaseDelay * 2^attempt) * rand.
func (r *RetryingExecutor) backoff(attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt))
	raw := time.Duration(float64(r.cfg.BaseDelay) * exp)
	if raw > r.cfg.MaxDelay {
		raw = r.cfg.MaxDelay
	}
	//nolint:gosec // jitter does not need crypto/rand
	jittered := time.Duration(rand.Int63n(int64(raw) + 1))
	return jittered
}

// classifyResult wraps a raw tool output string into a ToolResult,
// setting Retryable to true when the output matches known transient
// error patterns (timeouts, network errors, temporary failures).
func classifyResult(output string) ToolResult {
	return ToolResult{
		Output:    output,
		Retryable: isRetryableResult(output),
	}
}

// isRetryableResult returns true when the tool output string looks
// like a transient/retryable failure.
func isRetryableResult(output string) bool {
	var envelope struct {
		Error  string `json:"error"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
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
	"eof",
}
