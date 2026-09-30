package db

import (
	"context"
	"path/filepath"
	"testing"
)

// Every pooled connection must carry busy_timeout; otherwise concurrent
// writers get SQLITE_BUSY immediately and rows are dropped.
func TestOpenAppliesBusyTimeoutToEveryConnection(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	ctx := context.Background()
	const n = 4
	conns := make([]interface{ Close() error }, 0, n)
	for i := 0; i < n; i++ {
		c, err := d.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		var timeout, fk int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 || fk != 1 {
			t.Fatalf("conn %d: busy_timeout=%d foreign_keys=%d, want 5000/1", i, timeout, fk)
		}
	}
	for _, c := range conns {
		_ = c.Close()
	}
}
