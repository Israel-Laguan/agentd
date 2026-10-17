package services_test

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

// D2: with a policy set, a source_path outside the root is a bad plan and
// nothing is persisted (B-064).
func TestMaterializePlanRefusesSourceOutsidePolicyRoot(t *testing.T) {
	store := testutil.NewFakeStore()
	wsRoot := t.TempDir()
	store.SetProjectsDir(wsRoot)
	svc := services.NewProjectService(store, &sandbox.FSWorkspaceManager{Root: wsRoot}).
		WithSourcePolicy(sandbox.SourcePolicy{Root: t.TempDir()})

	_, _, err := svc.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "seeded",
		SourcePath:  t.TempDir(),
		Tasks:       []models.DraftTask{{Title: "t"}},
	})
	if !errors.Is(err, models.ErrInvalidDraftPlan) {
		t.Fatalf("err = %v, want ErrInvalidDraftPlan", err)
	}
	if projects, _ := store.ListProjects(context.Background()); len(projects) != 0 {
		t.Fatalf("refused plan persisted %d project(s)", len(projects))
	}
}
