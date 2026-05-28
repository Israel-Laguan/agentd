package services

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"agentd/internal/frontdesk"
)

// BreakerSnapshot is a read-only view of the LLM circuit breaker for the
// /system/status endpoint. We deliberately use primitive field types so
// that services can stay decoupled from internal/queue (which defines its
// own typed BreakerState alias).
type BreakerSnapshot struct {
	State        string        `json:"state"`
	FailureCount int           `json:"failure_count"`
	OpenFor      time.Duration `json:"open_for"`
	LastError    string        `json:"last_error,omitempty"`
}

// BreakerProbe is the consumer-side interface implemented by
// internal/queue.breakerProbe (a thin adapter wired in cmd/agentd).
// Returning primitives keeps services free of queue imports.
type BreakerProbe interface {
	State() string
	FailureCount() int
	OpenDuration() time.Duration
	LastError() error
}

// BreakerResetter allows an operator to reset the circuit breaker without
// restarting the daemon. It is intentionally separate from BreakerProbe to
// keep the read path decoupled from the write path.
type BreakerResetter interface {
	Reset()
}

// ProviderBreakersProbe surfaces per-provider breaker state for the status
// endpoint and supports targeted resets.
type ProviderBreakersProbe interface {
	Snapshot() map[string]ProviderBreakerEntry
	Reset(provider string)
	ResetAll()
}

// MemorySnapshot reports current Go runtime memory usage in bytes.
type MemorySnapshot struct {
	HeapAlloc uint64 `json:"heap_alloc"`
	HeapSys   uint64 `json:"heap_sys"`
	NumGC     uint32 `json:"num_gc"`
}

// ProviderBreakerEntry is the per-provider circuit breaker state exposed in
// the system status response. The shape mirrors safety.ProviderBreakerEntry
// but is defined here with JSON tags so services stays import-free from queue.
type ProviderBreakerEntry struct {
	State        string        `json:"state"`
	FailureCount int           `json:"failure_count"`
	OpenFor      time.Duration `json:"open_for"`
	LastError    string        `json:"last_error,omitempty"`
}

// SystemStatus is the payload returned by /api/v1/system/status.
type SystemStatus struct {
	Status           *frontdesk.StatusReport          `json:"status,omitempty"`
	Breaker          *BreakerSnapshot                 `json:"breaker,omitempty"`
	ProviderBreakers map[string]ProviderBreakerEntry   `json:"provider_breakers,omitempty"`
	Memory           MemorySnapshot                   `json:"memory"`
	BuiltAt          time.Time                        `json:"built_at"`
	TotalTokenUsage         int           `json:"total_token_usage"`
	RollingBudgetEnabled    bool          `json:"rolling_budget_enabled,omitempty"`
	RollingTokenLimit       int           `json:"rolling_token_limit,omitempty"`
	RollingTokenRemaining   int           `json:"rolling_token_remaining"`
	RollingTokenWindow      string        `json:"rolling_token_window,omitempty"`
}

// TokenCounter sums token usage across all persisted tasks.
// Implemented by the kanban store.
type TokenCounter interface {
	SumTokenUsage(ctx context.Context) (int, error)
}

// RollingBudgetProbe exposes rolling token budget state for /system/status.
type RollingBudgetProbe interface {
	RollingBudgetSnapshot() (limit, remaining int, enabled bool, window time.Duration)
}

// SystemService composes the deterministic StatusSummarizer with optional
// runtime probes. Probes are nil-safe so unit tests can construct the
// service without spinning up a daemon.
type SystemService struct {
	Summarizer       *frontdesk.StatusSummarizer
	Breaker          BreakerProbe
	Resetter         BreakerResetter
	ProviderBreakers ProviderBreakersProbe
	TokenCounter     TokenCounter
	RollingBudget    RollingBudgetProbe
	Now              func() time.Time
	ReadMem          func() MemorySnapshot
}

// NewSystemService wires the deterministic status summarizer and any
// available runtime probes.
func NewSystemService(summarizer *frontdesk.StatusSummarizer, breaker BreakerProbe) *SystemService {
	return &SystemService{Summarizer: summarizer, Breaker: breaker, Now: time.Now, ReadMem: readMemStats}
}

// StatusOptions controls filtering for the status snapshot.
type StatusOptions struct {
	IncludeHealing bool
	IncludeSystem  bool
}

// Snapshot collects the current system status. It always returns a
// non-nil SystemStatus on success; missing probes simply omit their
// sections.
func (s *SystemService) Snapshot(ctx context.Context) (*SystemStatus, error) {
	return s.SnapshotWithOptions(ctx, StatusOptions{IncludeHealing: true, IncludeSystem: true})
}

// SnapshotWithOptions collects the current system status with filtering.
func (s *SystemService) SnapshotWithOptions(ctx context.Context, opts StatusOptions) (*SystemStatus, error) {
	out := &SystemStatus{BuiltAt: s.now(), Memory: s.readMem()}
	if s.Summarizer != nil {
		report, err := s.Summarizer.SummarizeWithOptions(ctx, frontdesk.SummarizeOptions{
			IncludeHealing: opts.IncludeHealing,
			IncludeSystem:  opts.IncludeSystem,
		})
		if err != nil {
			return nil, err
		}
		out.Status = report
	}
	if s.Breaker != nil {
		snap := &BreakerSnapshot{
			State:        s.Breaker.State(),
			FailureCount: s.Breaker.FailureCount(),
			OpenFor:      s.Breaker.OpenDuration(),
		}
		if err := s.Breaker.LastError(); err != nil {
			snap.LastError = err.Error()
		}
		out.Breaker = snap
	}
	if s.ProviderBreakers != nil {
		raw := s.ProviderBreakers.Snapshot()
		if len(raw) > 0 {
			provSnap := make(map[string]ProviderBreakerEntry, len(raw))
			for name, e := range raw {
				provSnap[name] = ProviderBreakerEntry{
					State:        e.State,
					FailureCount: e.FailureCount,
					OpenFor:      e.OpenFor,
					LastError:    e.LastError,
				}
			}
			out.ProviderBreakers = provSnap
		}
	}
	if s.TokenCounter != nil {
		if total, err := s.TokenCounter.SumTokenUsage(ctx); err == nil {
			out.TotalTokenUsage = total
		}
	}
	if s.RollingBudget != nil {
		limit, remaining, enabled, window := s.RollingBudget.RollingBudgetSnapshot()
		if enabled {
			out.RollingBudgetEnabled = true
			out.RollingTokenLimit = limit
			out.RollingTokenRemaining = remaining
			out.RollingTokenWindow = window.String()
		}
	}
	return out, nil
}

// ResetBreaker resets the global circuit breaker via the injected Resetter.
// Returns an error if no resetter was wired.
func (s *SystemService) ResetBreaker(provider string) error {
	if provider != "" {
		if s.ProviderBreakers == nil {
			return fmt.Errorf("per-provider breaker reset not available")
		}
		s.ProviderBreakers.Reset(provider)
		return nil
	}
	// Reset all: global breaker + all per-provider breakers.
	if s.Resetter == nil && s.ProviderBreakers == nil {
		return fmt.Errorf("breaker reset not available")
	}
	if s.Resetter != nil {
		s.Resetter.Reset()
	}
	if s.ProviderBreakers != nil {
		s.ProviderBreakers.ResetAll()
	}
	return nil
}

func (s *SystemService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SystemService) readMem() MemorySnapshot {
	if s.ReadMem != nil {
		return s.ReadMem()
	}
	return readMemStats()
}

func readMemStats() MemorySnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return MemorySnapshot{HeapAlloc: stats.HeapAlloc, HeapSys: stats.HeapSys, NumGC: stats.NumGC}
}
