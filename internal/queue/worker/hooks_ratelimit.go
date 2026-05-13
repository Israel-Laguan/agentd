package worker

import (
	"fmt"
	"sync"
)

// RateLimitCounter abstracts per-tool invocation counting so that
// callers can swap in alternative implementations (e.g., time-windowed
// or externally-backed stores) without changing the hook.
type RateLimitCounter interface {
	Increment(tool string) int
	Count(tool string) int
}

// RateLimitStore is the default in-memory RateLimitCounter. It tracks
// per-tool invocation counts for a single session. All methods are safe
// for concurrent use.
type RateLimitStore struct {
	mu       sync.Mutex
	counters map[string]int
}

// compile-time interface check
var _ RateLimitCounter = (*RateLimitStore)(nil)

// NewRateLimitStore returns an empty RateLimitStore ready for use.
func NewRateLimitStore() *RateLimitStore {
	return &RateLimitStore{counters: make(map[string]int)}
}

// Increment atomically bumps the counter for the given tool and returns
// the new count.
func (s *RateLimitStore) Increment(tool string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[tool]++
	return s.counters[tool]
}

// Count returns the current invocation count for the given tool.
func (s *RateLimitStore) Count(tool string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters[tool]
}

// resolveLimit returns the configured limit for the tool, falling back
// to the "default" key. Zero means unlimited.
func resolveLimit(limits map[string]int, tool string) int {
	if v, ok := limits[tool]; ok {
		return v
	}
	return limits["default"]
}

// RateLimitHook returns a PreHook that enforces per-tool call limits
// within a single session. The limits map keys are tool names (plus the
// special "default" key); values are the maximum allowed calls (0 =
// unlimited). The store must be unique per session so counters are
// isolated across workers. A nil store with non-empty limits is treated
// as a misconfiguration and vetoes all calls.
func RateLimitHook(limits map[string]int, store RateLimitCounter) PreHook {
	return PreHook{
		Name:   "rate-limit",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if len(limits) == 0 {
				return HookVerdict{}, nil
			}
			if store == nil {
				return HookVerdict{
					Veto:   true,
					Reason: "Rate limit store is not configured.",
				}, nil
			}
			limit := resolveLimit(limits, ctx.ToolName)
			if limit == 0 {
				return HookVerdict{}, nil
			}
			if limit < 0 {
				return HookVerdict{
					Veto:   true,
					Reason: fmt.Sprintf("Invalid negative rate limit for tool %q (%d).", ctx.ToolName, limit),
				}, nil
			}
			count := store.Increment(ctx.ToolName)
			if count > limit {
				return HookVerdict{
					Veto: true,
					Reason: fmt.Sprintf(
						"Rate limit exceeded for tool %q (%d/%d). Consider consolidating commands.",
						ctx.ToolName, count-1, limit,
					),
				}, nil
			}
			return HookVerdict{}, nil
		},
	}
}
