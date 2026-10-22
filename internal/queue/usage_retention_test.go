package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/testutil"
)

type fakePruner struct {
	cutoffs []time.Time
	err     error
}

func (f *fakePruner) PruneUsageBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	return 3, f.err
}

// prunerStore is a fake store that also prunes, so pruneUsage's type
// assertion is the thing under test.
type prunerStore struct {
	*testutil.FakeKanbanStore
	pruner *fakePruner
}

func (s prunerStore) PruneUsageBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.pruner.PruneUsageBefore(ctx, cutoff)
}

func TestPruneUsageUsesTheRetentionCutoff(t *testing.T) {
	t.Parallel()
	p := &fakePruner{}
	d := &Daemon{store: prunerStore{FakeKanbanStore: testutil.NewFakeStore(), pruner: p}, usageRetentionDays: 14}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	d.pruneUsage(context.Background(), now)
	if len(p.cutoffs) != 1 || !p.cutoffs[0].Equal(time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("cutoffs = %v, want one cut 14 days before now", p.cutoffs)
	}
}

func TestPruneUsageKeepsEverythingWhenRetentionIsZero(t *testing.T) {
	t.Parallel()
	p := &fakePruner{}
	d := &Daemon{store: prunerStore{FakeKanbanStore: testutil.NewFakeStore(), pruner: p}, usageRetentionDays: 0}
	d.pruneUsage(context.Background(), time.Now())
	if len(p.cutoffs) != 0 {
		t.Fatalf("pruned with retention 0: %v", p.cutoffs)
	}
}

func TestPruneUsageFailureIsLoggedNotFatal(t *testing.T) {
	t.Parallel()
	p := &fakePruner{err: errors.New("db locked")}
	d := &Daemon{store: prunerStore{FakeKanbanStore: testutil.NewFakeStore(), pruner: p}, usageRetentionDays: 7}
	d.pruneUsage(context.Background(), time.Now())
	if len(p.cutoffs) != 1 {
		t.Fatalf("prune was not attempted: %v", p.cutoffs)
	}
}
