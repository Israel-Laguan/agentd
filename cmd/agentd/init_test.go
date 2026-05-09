package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInitCreatesHomeDatabaseAndWAL(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agentd init error = %v", err)
	}

	assertPathExists(t, home)
	dbPath := filepath.Join(home, "global.db")
	assertPathExists(t, dbPath)
	assertPathExists(t, filepath.Join(home, "agentd.crontab"))
	assertJournalMode(t, dbPath, "wal")
}

func TestInitDoesNotOverwriteUserCron(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("first agentd init error = %v", err)
	}

	cronPath := filepath.Join(home, "agentd.crontab")
	userCron := "@every 11s intake\n"
	if err := os.WriteFile(cronPath, []byte(userCron), 0o644); err != nil {
		t.Fatalf("write user cron: %v", err)
	}

	cmd = newRootCommand()
	cmd.SetArgs([]string{"--home", home, "init"})
	output.Reset()
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("second agentd init error = %v", err)
	}

	contents, err := os.ReadFile(cronPath)
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if string(contents) != userCron {
		t.Fatalf("cron file = %q, want %q", contents, userCron)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path %s to exist: %v", path, err)
	}
}

func assertJournalMode(t *testing.T, dbPath, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() { _ = db.Close() }()

	var got string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&got); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("journal_mode = %s, want %s", got, want)
	}
}
