//go:build busywritespike

package kanban

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentd/internal/models"
)

// SP-009 (B-001) load test: hold the SQLite write lock past the 5s
// busy_timeout and measure what the token ledger write path loses.
//
// Build-tagged because it holds the write lock for 8s on purpose; `make test`
// does not pass `-tags=busywritespike`. Run it directly:
//
//	go test ./internal/kanban/ -tags=busywritespike -run TestSpike -v -count=1
//
// Every case asserts the durable counter equals the successful write count, so
// a drop shows up as a mismatch rather than only in the log.

const (
	spikeLockHold  = 8 * time.Second
	spikeWorkers   = 8
	spikePerWorker = 20
)

// TestSpikeBusyWriteUnderLongLock runs the ledger counter write with no retry
// wrapper -- this is the pre-T-031 behaviour, kept as the "before" measurement.
func TestSpikeBusyWriteUnderLongLock(t *testing.T) {
	store, taskID, db := newSpikeStore(t, "unwrapped")
	defer store.Close()

	held := spikeHoldWriteLock(t, db)

	var ok, failed int64
	var firstFailure time.Duration
	start := time.Now()
	runSpikeWrites(t, func(ctx context.Context) error {
		_, err := db.ExecContext(ctx,
			`UPDATE tasks SET token_usage = token_usage + ? WHERE id = ?`, 1, taskID)
		return err
	}, func(err error) {
		atomic.AddInt64(&failed, 1)
		if firstFailure == 0 {
			firstFailure = time.Since(start)
			t.Logf("first failure after %v: %v", firstFailure.Truncate(time.Millisecond), err)
		}
	}, &ok)

	<-held
	t.Logf("RESULT unwrapped attempts=%d ok=%d failed=%d elapsed=%v first_failure=%v (busy_timeout=5s)",
		ok+failed, ok, failed, time.Since(start).Truncate(time.Millisecond),
		firstFailure.Truncate(time.Millisecond))
	spikeAssertNoSilentLoss(t, store, db, taskID, ok)
}

// TestSpikeBusyWriteWithRetryOnBusy runs the same load through the real
// store.AddTokenUsage, which T-031 wrapped in retryOnBusyNoResult. This is the
// shipped behaviour, so it is the one that must show zero loss.
func TestSpikeBusyWriteWithRetryOnBusy(t *testing.T) {
	store, taskID, db := newSpikeStore(t, "retried")
	defer store.Close()

	held := spikeHoldWriteLock(t, db)

	var ok, failed int64
	start := time.Now()
	runSpikeWrites(t, func(ctx context.Context) error {
		return store.AddTokenUsage(ctx, taskID, 1)
	}, func(err error) {
		atomic.AddInt64(&failed, 1)
		t.Logf("failed: %v", err)
	}, &ok)

	<-held
	t.Logf("RESULT retried attempts=%d ok=%d failed=%d elapsed=%v",
		ok+failed, ok, failed, time.Since(start).Truncate(time.Millisecond))
	spikeAssertNoSilentLoss(t, store, db, taskID, ok)
}

// TestSpikeRetryOnBusyCeiling shows the retry wrapper's real budget is
// attempts x busy_timeout (~30s), not the ~156ms of backoff. Holds the lock
// past that ceiling and expects writes to fail again.
func TestSpikeRetryOnBusyCeiling(t *testing.T) {
	store, taskID, db := newSpikeStore(t, "ceiling")
	defer store.Close()

	held := spikeHoldWriteLock(t, db, 32*time.Second)

	var ok, failed int64
	start := time.Now()
	runSpikeWrites(t, func(ctx context.Context) error {
		return retryOnBusyNoResult(ctx, func(ctx context.Context) error {
			_, err := db.ExecContext(ctx,
				`UPDATE tasks SET token_usage = token_usage + ? WHERE id = ?`, 1, taskID)
			return err
		})
	}, func(err error) {
		atomic.AddInt64(&failed, 1)
	}, &ok)

	<-held
	t.Logf("RESULT ceiling_32s_hold attempts=%d ok=%d failed=%d elapsed=%v "+
		"(6 attempts x 5s busy_timeout ~= 30s)", ok+failed, ok, failed,
		time.Since(start).Truncate(time.Millisecond))
	spikeAssertNoSilentLoss(t, store, db, taskID, ok)
}

// TestSpikeRetryOnBusyBackoffCost measures RetryOnBusy in isolation: how many
// attempts and how much wall clock when every attempt returns BUSY.
func TestSpikeRetryOnBusyBackoffCost(t *testing.T) {
	var attempts int64
	start := time.Now()
	_, err := retryOnBusy(context.Background(), func(ctx context.Context) (int, error) {
		atomic.AddInt64(&attempts, 1)
		return 0, errSpikeBusy
	})
	t.Logf("RESULT backoff attempts=%d wall=%v returned_err=%v",
		attempts, time.Since(start).Truncate(time.Millisecond), err != nil)
}

var errSpikeBusy = &spikeBusyErr{}

type spikeBusyErr struct{}

func (*spikeBusyErr) Error() string { return "database is locked (5) (SQLITE_BUSY)" }
func (*spikeBusyErr) Code() int     { return 5 }

func newSpikeStore(t *testing.T, name string) (*Store, string, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(dir+"/"+name+".db", dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	store := NewStore(db, dir)
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "spike " + name,
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	return store, tasks[0].ID, db
}

// spikeHoldWriteLock takes the write lock and holds it for hold, returning a
// channel closed once the transaction is rolled back.
func spikeHoldWriteLock(t *testing.T, db *sql.DB, hold ...time.Duration) <-chan struct{} {
	t.Helper()
	d := spikeLockHold
	if len(hold) > 0 {
		d = hold[0]
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tx, err := beginImmediate(context.Background(), db)
		if err != nil {
			return
		}
		defer func() { _ = tx.Rollback() }()
		time.Sleep(d)
	}()
	// Let the holder win the lock before the workers start.
	time.Sleep(250 * time.Millisecond)
	return done
}

func runSpikeWrites(t *testing.T, write func(context.Context) error, onErr func(error), ok *int64) {
	t.Helper()
	var wg sync.WaitGroup
	for w := 0; w < spikeWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for i := 0; i < spikePerWorker; i++ {
				if err := write(ctx); err != nil {
					onErr(err)
					continue
				}
				atomic.AddInt64(ok, 1)
			}
		}()
	}
	wg.Wait()
}

func spikeAssertNoSilentLoss(t *testing.T, store *Store, db *sql.DB, taskID string, ok int64) {
	t.Helper()
	ctx := context.Background()
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	t.Logf("RESULT durable_token_usage=%d successful_writes=%d lost=%d",
		task.TokenUsage, ok, ok-int64(task.TokenUsage))
	if int64(task.TokenUsage) != ok {
		t.Errorf("durable token_usage = %d, want %d: the counter and the successful "+
			"writes disagree, so a write was lost or double-counted", task.TokenUsage, ok)
	}
	var events int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE task_id = ? AND type = ?",
		taskID, models.EventTypeTokenUsage).Scan(&events); err != nil {
		t.Fatalf("count token usage events: %v", err)
	}
	t.Logf("RESULT token_usage_events=%d (the Emit path drops these silently)", events)
}
