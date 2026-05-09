package services

import (
	"context"
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

// MemorySnapshot reports current Go runtime memory usage in bytes.
type MemorySnapshot struct {
	HeapAlloc uint64 `json:"heap_alloc"`
	HeapSys   uint64 `json:"heap_sys"`
	NumGC     uint32 `json:"num_gc"`
}

// SystemStatus is the payload returned by /api/v1/system/status.
type SystemStatus struct {
	Status  *frontdesk.StatusReport `json:"status,omitempty"`
	Breaker *BreakerSnapshot        `json:"breaker,omitempty"`
	Memory  MemorySnapshot          `json:"memory"`
	BuiltAt time.Time               `json:"built_at"`
}

// SystemService composes the deterministic StatusSummarizer with optional
// runtime probes. Probes are nil-safe so unit tests can construct the
// service without spinning up a daemon.
type SystemService struct {
	Summarizer *frontdesk.StatusSummarizer
	Breaker    BreakerProbe
	Now        func() time.Time
	ReadMem    func() MemorySnapshot
}

// NewSystemService wires the deterministic status summarizer and any
// available runtime probes.
func NewSystemService(summarizer *frontdesk.StatusSummarizer, breaker BreakerProbe) *SystemService {
	return &SystemService{Summarizer: summarizer, Breaker: breaker, Now: time.Now, ReadMem: readMemStats}
}

// Snapshot collects the current system status. It always returns a
// non-nil SystemStatus on success; missing probes simply omit their
// sections.
func (s *SystemService) Snapshot(ctx context.Context) (*SystemStatus, error) {
	out := &SystemStatus{BuiltAt: s.now(), Memory: s.readMem()}
	if s.Summarizer != nil {
		report, err := s.Summarizer.Summarize(ctx)
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
	return out, nil
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
