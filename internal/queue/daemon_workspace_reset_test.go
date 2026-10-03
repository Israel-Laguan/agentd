package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// B-010: DaemonOptions.WorkspaceReset is what recovery.clean_workspace_on_recover
// turns into; Start must hand it to boot reconcile, and nil must leave the
// interrupted attempt's files in place.
func TestDaemonStart_WorkspaceResetOnRecover(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enabled   bool
		wantFiles bool
	}{
		{"flag on resets the workspace", true, false},
		{"flag off keeps it", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store := testutil.NewFakeStore()
			store.SetProjectsDir(root)
			project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
				ProjectName: "p", StartEmptyWorkspace: true,
				Tasks: []models.DraftTask{{TempID: "a", Title: "A"}},
			})
			if err != nil {
				t.Fatalf("MaterializePlan: %v", err)
			}
			ws := &sandbox.FSWorkspaceManager{Root: root}
			dir, err := ws.EnsureProjectDir(ctx, project.ID)
			if err != nil {
				t.Fatalf("EnsureProjectDir: %v", err)
			}
			partial := filepath.Join(dir, "first-attempt.txt")
			if err := os.WriteFile(partial, []byte("partial"), 0o644); err != nil {
				t.Fatalf("write partial: %v", err)
			}
			if _, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 999999); err != nil {
				t.Fatalf("MarkTaskRunning: %v", err)
			}
			opts := DaemonOptions{
				TaskInterval: time.Hour, MaxTaskInterval: time.Hour, IntakeInterval: time.Hour,
				HeartbeatInterval: time.Hour, Probe: StaticPIDProbe{},
			}
			if tc.enabled {
				opts.WorkspaceReset = ws
			}
			daemon := NewDaemon(store, nil, nil, nil, nil, opts)
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- daemon.Start(runCtx) }()
			time.Sleep(30 * time.Millisecond)
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("Start() error = %v", err)
			}

			_, statErr := os.Stat(partial)
			if exists := statErr == nil; exists != tc.wantFiles {
				t.Fatalf("first attempt's file exists = %v, want %v", exists, tc.wantFiles)
			}
		})
	}
}
