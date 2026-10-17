package main

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

// The runtime's project service must carry api.source_root; without it every
// host directory could seed a project (B-064, D2).
func TestNewRuntimeDepsAppliesSourceRoot(t *testing.T) {
	cfg := config.Config{ProjectsDir: t.TempDir(), HomeDir: t.TempDir()}
	cfg.API.SourceRoot = t.TempDir()

	deps, err := newRuntimeDeps(cfg, nil)
	if err != nil {
		t.Fatalf("newRuntimeDeps() error = %v", err)
	}
	_, _, err = deps.project.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "seeded", SourcePath: t.TempDir(), Tasks: []models.DraftTask{{Title: "t"}},
	})
	if !errors.Is(err, models.ErrInvalidDraftPlan) {
		t.Fatalf("err = %v, want ErrInvalidDraftPlan for a source outside api.source_root", err)
	}
}
