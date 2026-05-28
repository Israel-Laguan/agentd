package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/frontdesk"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

type stubBreaker struct {
	state     string
	failCount int
	open      time.Duration
	lastErr   error
}

func (b stubBreaker) State() string               { return b.state }
func (b stubBreaker) FailureCount() int           { return b.failCount }
func (b stubBreaker) OpenDuration() time.Duration { return b.open }
func (b stubBreaker) LastError() error            { return b.lastErr }

func TestSystemServiceSnapshotDefaultNowAndMem(t *testing.T) {
	svc := &services.SystemService{
		Summarizer: nil,
		Breaker:    nil,
		Now:        nil,
		ReadMem:    nil,
	}
	out, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if out.BuiltAt.IsZero() {
		t.Fatal("BuiltAt should be populated via default Now()")
	}
	if out.Memory.HeapSys == 0 && out.Memory.HeapAlloc == 0 {
		t.Fatalf("memory snapshot empty: %#v", out.Memory)
	}
}

func TestSystemServiceSnapshotBreakerWithoutLastError(t *testing.T) {
	br := stubBreaker{state: "closed", failCount: 0}
	svc := services.NewSystemService(nil, br)
	svc.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	svc.ReadMem = func() services.MemorySnapshot { return services.MemorySnapshot{} }

	out, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if out.Breaker == nil || out.Breaker.State != "closed" {
		t.Fatalf("breaker = %#v", out.Breaker)
	}
	if out.Breaker.LastError != "" {
		t.Fatalf("LastError = %q, want empty", out.Breaker.LastError)
	}
}

func TestSystemServiceSnapshotNoSummarizer(t *testing.T) {
	svc := &services.SystemService{
		Summarizer: nil,
		Breaker:    nil,
		Now:        func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		ReadMem: func() services.MemorySnapshot {
			return services.MemorySnapshot{HeapAlloc: 42, HeapSys: 99, NumGC: 3}
		},
	}
	out, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if out.Status != nil {
		t.Fatalf("expected no status")
	}
	if out.Breaker != nil {
		t.Fatalf("expected no breaker")
	}
	if out.Memory.HeapAlloc != 42 || out.Memory.NumGC != 3 {
		t.Fatalf("memory = %#v", out.Memory)
	}
	if !out.BuiltAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("BuiltAt = %v", out.BuiltAt)
	}
}

func TestSystemServiceSnapshotWithSummarizerAndBreaker(t *testing.T) {
	store := testutil.NewFakeStore()
	sum := frontdesk.NewStatusSummarizer(store)
	br := stubBreaker{state: "open", failCount: 2, open: time.Minute, lastErr: errors.New("boom")}
	svc := services.NewSystemService(sum, br)
	svc.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	svc.ReadMem = func() services.MemorySnapshot { return services.MemorySnapshot{HeapAlloc: 1} }

	out, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if out.Status == nil || out.Status.Kind != "status_report" {
		t.Fatalf("status = %#v", out.Status)
	}
	if out.Breaker == nil || out.Breaker.State != "open" || out.Breaker.FailureCount != 2 {
		t.Fatalf("breaker = %#v", out.Breaker)
	}
	if out.Breaker.OpenFor != time.Minute || out.Breaker.LastError != "boom" {
		t.Fatalf("breaker fields: %#v", out.Breaker)
	}
}

func TestSystemServiceSnapshotSummarizerError(t *testing.T) {
	errStore := &listProjectsFailStore{FakeKanbanStore: testutil.NewFakeStore()}
	svc := services.NewSystemService(frontdesk.NewStatusSummarizer(errStore), nil)

	_, err := svc.Snapshot(context.Background())
	if err == nil || err.Error() != "list boom" {
		t.Fatalf("expected list error, got %v", err)
	}
}

type listProjectsFailStore struct {
	*testutil.FakeKanbanStore
}

func (s *listProjectsFailStore) ListProjects(context.Context) ([]models.Project, error) {
	return nil, errors.New("list boom")
}

// stubTokenCounter is a test double for services.TokenCounter.
type stubTokenCounter struct{ total int }

func (s stubTokenCounter) SumTokenUsage(context.Context) (int, error) { return s.total, nil }

// TestSystemStatusTotalTokenUsage verifies that SnapshotWithOptions includes
// the sum of all task token_usage values when a TokenCounter is wired.
func TestSystemStatusTotalTokenUsage(t *testing.T) {
	t.Parallel()
	svc := &services.SystemService{
		Now:          func() time.Time { return time.Unix(0, 0).UTC() },
		ReadMem:      func() services.MemorySnapshot { return services.MemorySnapshot{} },
		TokenCounter: stubTokenCounter{total: 47},
	}
	out, err := svc.SnapshotWithOptions(context.Background(), services.StatusOptions{})
	if err != nil {
		t.Fatalf("SnapshotWithOptions: %v", err)
	}
	if out.TotalTokenUsage != 47 {
		t.Errorf("TotalTokenUsage = %d, want 47", out.TotalTokenUsage)
	}
}

// TestSystemStatusTotalTokenUsage_NilCounter verifies that a nil TokenCounter
// leaves TotalTokenUsage at zero without error.
func TestSystemStatusTotalTokenUsage_NilCounter(t *testing.T) {
	t.Parallel()
	svc := &services.SystemService{
		Now:     func() time.Time { return time.Unix(0, 0).UTC() },
		ReadMem: func() services.MemorySnapshot { return services.MemorySnapshot{} },
	}
	out, err := svc.SnapshotWithOptions(context.Background(), services.StatusOptions{})
	if err != nil {
		t.Fatalf("SnapshotWithOptions: %v", err)
	}
	if out.TotalTokenUsage != 0 {
		t.Errorf("TotalTokenUsage = %d, want 0", out.TotalTokenUsage)
	}
}

type stubRollingBudget struct {
	limit, remaining int
	enabled          bool
	window           time.Duration
}

func (s stubRollingBudget) RollingBudgetSnapshot() (limit, remaining int, enabled bool, window time.Duration) {
	return s.limit, s.remaining, s.enabled, s.window
}

func TestSystemStatusRollingBudget(t *testing.T) {
	t.Parallel()
	svc := &services.SystemService{
		Now:     func() time.Time { return time.Unix(0, 0).UTC() },
		ReadMem: func() services.MemorySnapshot { return services.MemorySnapshot{} },
		RollingBudget: stubRollingBudget{
			limit: 100_000, remaining: 42_000, enabled: true, window: 5 * time.Hour,
		},
	}
	out, err := svc.SnapshotWithOptions(context.Background(), services.StatusOptions{})
	if err != nil {
		t.Fatalf("SnapshotWithOptions: %v", err)
	}
	if !out.RollingBudgetEnabled || out.RollingTokenLimit != 100_000 || out.RollingTokenRemaining != 42_000 {
		t.Fatalf("rolling budget snapshot = %+v", out)
	}
	if out.RollingTokenWindow != (5 * time.Hour).String() {
		t.Fatalf("RollingTokenWindow = %v, want 5h0m0s", out.RollingTokenWindow)
	}
}
