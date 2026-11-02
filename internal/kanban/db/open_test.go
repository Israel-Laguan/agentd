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

// Opens that upgrade one database at once, as the daemon and a CLI command can
// during an upgrade, must all succeed. Each saw the v28 column missing and
// added it, and every open but the first failed on a duplicate column.
func TestConcurrentOpensUpgradeOneDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	for round := 0; round < 10; round++ {
		d, err := Open(path, dir)
		if err != nil {
			t.Fatal(err)
		}
		// Back to v27: the column v28 adds is gone.
		for _, stmt := range []string{`ALTER TABLE agent_profiles DROP COLUMN gated_tools`,
			`UPDATE settings SET value = '27' WHERE key = 'schema_version'`} {
			if _, err := d.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
		_ = d.Close()

		const opens = 8
		start := make(chan struct{})
		errs := make(chan error, opens)
		for i := 0; i < opens; i++ {
			go func() {
				<-start
				d, err := Open(path, dir)
				if err == nil {
					_ = d.Close()
				}
				errs <- err
			}()
		}
		close(start)
		for i := 0; i < opens; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: concurrent open: %v", round, err)
			}
		}
	}
}
