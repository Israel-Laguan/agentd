package worker

import (
	"fmt"
	"sync"
)

// RateLimitStore tracks per-tool invocation counts for a single session.
// All methods are safe for concurrent use. The store is designed to
// support both simple counter-based and future time-windowed modes.
type RateLimitStore struct {
	mu       sync.Mutex
	counters map[string]int
}

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
// isolated across workers.
func RateLimitHook(limits map[string]int, store *RateLimitStore) PreHook {
	return PreHook{
		Name:   "rate-limit",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if store == nil || len(limits) == 0 {
				return HookVerdict{}, nil
			}
			limit := resolveLimit(limits, ctx.ToolName)
			if limit <= 0 {
				return HookVerdict{}, nil
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
