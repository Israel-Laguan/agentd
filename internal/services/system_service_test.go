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
	state        string
	failCount    int
	open         time.Duration
	lastErr      error
}

func (b stubBreaker) State() string                  { return b.state }
func (b stubBreaker) FailureCount() int              { return b.failCount }
func (b stubBreaker) OpenDuration() time.Duration    { return b.open }
func (b stubBreaker) LastError() error             { return b.lastErr }

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
