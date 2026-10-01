package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// T-031 / SP-009. The reason the token ledger writes are wrapped in
// RetryOnBusy even though the 5s busy_timeout is far longer than RetryOnBusy's
// ~156ms of backoff: every retry attempt gets a *fresh* busy_timeout window, so
// the wrapper multiplies lock tolerance by the attempt count.
//
// This shrinks busy_timeout so the suite stays fast. Production uses 5000, and
// initialize() hardcodes PRAGMA busy_timeout=5000 on one connection, so the
// timeout has to be lowered per connection rather than through the DSN.

func shortBusyConn(t *testing.T, d *sql.DB, ms int) *sql.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout="+itoa(ms)); err != nil {
		t.Fatal(err)
	}
	return conn
}

// holdWriteLock takes the write lock on a dedicated connection and returns it.
// The lock is released when the test ends.
func holdWriteLock(t *testing.T, d *sql.DB, ms int) *sql.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout="+itoa(ms)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("take the write lock: %v", err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") })
	return conn
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestRetryOnBusyToleratesLockHeldLongerThanOneTimeout is the regression guard
// for the SP-009 finding: one unwrapped write cannot outlast a single
// busy_timeout, so it fails; the identical write through RetryOnBusy succeeds,
// and only because each attempt gets its own timeout window.
// openBusyProbeDB returns a DB with table t seeded with one row, plus a
// writer connection whose busy_timeout is ms.
func openBusyProbeDB(t *testing.T, ms int) (*sql.DB, *sql.Conn) {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()
	if _, err := d.ExecContext(ctx, "CREATE TABLE t(i INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO t VALUES (0)"); err != nil {
		t.Fatal(err)
	}
	return d, shortBusyConn(t, d, ms)
}

func TestRetryOnBusyToleratesLockHeldLongerThanOneTimeout(t *testing.T) {
	const busyMS = 200
	d, writerConn := openBusyProbeDB(t, busyMS)
	ctx := context.Background()

	// Take the write lock on this goroutine and hold it for ~3x a single
	// busy_timeout: past what one attempt can absorb, well inside what
	// maxBusyRetries attempts can.
	holder := holdWriteLock(t, d, busyMS)

	unwrappedCh := make(chan error, 1)
	wrappedCh := make(chan error, 1)
	start := time.Now()

	// Baseline: one unwrapped attempt, no retry. Must fail while the lock is held.
	go func() {
		_, err := writerConn.ExecContext(ctx, `INSERT INTO t VALUES (1)`)
		unwrappedCh <- err
	}()

	// Same write through the wrapper. Must land once the lock is released.
	go func() {
		wrappedCh <- RetryOnBusyNoResult(ctx, func(ctx context.Context) error {
			_, err := writerConn.ExecContext(ctx, `INSERT INTO t VALUES (2)`)
			return err
		})
	}()

	// Hold past what a single attempt can absorb, then release.
	time.Sleep(3 * busyMS * time.Millisecond)
	unwrappedErr := <-unwrappedCh
	if _, err := holder.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("release the write lock: %v", err)
	}

	if unwrappedErr == nil {
		t.Fatal("unwrapped write succeeded while the write lock was held; " +
			"the test would not prove anything")
	}
	t.Logf("unwrapped attempt failed after %v: %v",
		time.Since(start).Truncate(time.Millisecond), unwrappedErr)

	if err := <-wrappedCh; err != nil {
		t.Fatalf("wrapped write failed: %v", err)
	}
	t.Logf("wrapped write succeeded (busy_timeout=%dms, lock held %dms)",
		busyMS, 3*busyMS)

	var n int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM t").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rows = %d, want 2: the seed plus exactly the retried write. A retry "+
			"that replayed a committed write would double-count here", n)
	}
}
