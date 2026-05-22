package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentd/internal/kanban"
)

func (s *bootstrapScenario) homeDirExists(_ context.Context) error {
	if _, err := os.Stat(s.homeDir); err != nil {
		return fmt.Errorf("home directory missing: %w", err)
	}
	return nil
}

func (s *bootstrapScenario) dbFileExists(_ context.Context) error {
	if _, err := os.Stat(filepath.Join(s.homeDir, "global.db")); err != nil {
		return fmt.Errorf("database file missing: %w", err)
	}
	return nil
}

func (s *bootstrapScenario) dbWALMode(_ context.Context) error {
	db, err := sql.Open("sqlite", filepath.Join(s.homeDir, "global.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&mode); err != nil {
		return fmt.Errorf("query journal_mode: %w", err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("journal_mode = %q, want wal", mode)
	}
	return nil
}

func (s *bootstrapScenario) cronFileExists(_ context.Context) error {
	if _, err := os.Stat(filepath.Join(s.homeDir, "agentd.crontab")); err != nil {
		return fmt.Errorf("crontab file missing: %w", err)
	}
	return nil
}

func (s *bootstrapScenario) profilesSeeded(ctx context.Context) error {
	store, err := kanban.OpenStore(filepath.Join(s.homeDir, "global.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = store.Close() }()
	for _, id := range []string{"default", "researcher", "qa"} {
		profile, err := store.GetAgentProfile(ctx, id)
		if err != nil {
			return fmt.Errorf("%s profile lookup: %w", id, err)
		}
		if profile == nil {
			return fmt.Errorf("%s agent profile not found", id)
		}
	}
	return nil
}

func (s *bootstrapScenario) outputContains(_ context.Context, sub string) error {
	if !strings.Contains(s.lastOutput.String(), sub) {
		return fmt.Errorf("output does not contain %q:\n%s", sub, s.lastOutput.String())
	}
	return nil
}

func (s *bootstrapScenario) startFailed(_ context.Context) error {
	if s.lastErr == nil {
		return fmt.Errorf("expected start to fail but it succeeded")
	}
	return nil
}

func (s *bootstrapScenario) errorDescribesLLMConfig(_ context.Context) error {
	summary, hint := describeCommandError(s.lastErr)
	combined := strings.ToLower(summary + " " + hint)
	if !strings.Contains(combined, "llm") && !strings.Contains(combined, "provider") {
		return fmt.Errorf("expected LLM/provider hint; summary=%q hint=%q err=%v", summary, hint, s.lastErr)
	}
	return nil
}

func (s *bootstrapScenario) errorDescribesWritePermissions(_ context.Context) error {
	summary, hint := describeCommandError(s.lastErr)
	combined := strings.ToLower(summary + " " + hint)
	if !strings.Contains(combined, "writ") && !strings.Contains(combined, "permiss") {
		return fmt.Errorf("expected write/permission hint; summary=%q hint=%q err=%v", summary, hint, s.lastErr)
	}
	return nil
}

func (s *bootstrapScenario) errorDescribesWarmup(_ context.Context) error {
	summary, hint := describeCommandError(s.lastErr)
	combined := strings.ToLower(summary + " " + hint)
	if !strings.Contains(combined, "warmup") {
		return fmt.Errorf("expected warmup hint; summary=%q hint=%q err=%v", summary, hint, s.lastErr)
	}
	return nil
}

func (s *bootstrapScenario) errorDescribesAddressConflict(_ context.Context) error {
	summary, hint := describeCommandError(s.lastErr)
	combined := strings.ToLower(summary + " " + hint)
	if !strings.Contains(combined, "address") && !strings.Contains(combined, "port") {
		return fmt.Errorf("expected address/port hint; summary=%q hint=%q err=%v", summary, hint, s.lastErr)
	}
	return nil
}
