package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

// B-010: a project records whether its workspace was created empty, because that
// is the only starting state agentd can restore. A seeded or hand-populated
// project must read back as not started empty.
func TestMaterializePlanPersistsStartedEmpty(t *testing.T) {
	tests := []struct {
		name string
		plan models.DraftPlan
		want bool
	}{
		{"start empty", models.DraftPlan{StartEmptyWorkspace: true}, true},
		{"hand-populated (explicit ready)", models.DraftPlan{}, false},
		{"seeded from source_path", models.DraftPlan{SourcePath: "/some/source"}, false},
		{"start empty but also seeded", models.DraftPlan{StartEmptyWorkspace: true, SourcePath: "/some/source"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			plan := tt.plan
			plan.ProjectName = "p"
			plan.Tasks = []models.DraftTask{{TempID: "a", Title: "A"}}

			created, _, err := store.MaterializePlan(ctx, plan)
			if err != nil {
				t.Fatalf("MaterializePlan() error = %v", err)
			}
			got, err := store.GetProject(ctx, created.ID)
			if err != nil {
				t.Fatalf("GetProject() error = %v", err)
			}
			if got.StartedEmpty != tt.want {
				t.Fatalf("GetProject().StartedEmpty = %v, want %v", got.StartedEmpty, tt.want)
			}
			listed, err := store.ListProjects(ctx)
			if err != nil || len(listed) != 1 || listed[0].StartedEmpty != tt.want {
				t.Fatalf("ListProjects() = %#v, %v; want StartedEmpty %v", listed, err, tt.want)
			}
		})
	}
}

// A project materialized empty can still be seeded by hand afterwards (rsync plus
// POST /workspace/ready). Once that happens the bit is wrong — recovery would read
// the operator's content as a leftover and delete it — so marking the workspace
// ready has to clear it.
func TestClearProjectStartedEmptyAfterHandSeeding(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p", StartEmptyWorkspace: true,
		Tasks: []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	if err := store.ClearProjectStartedEmpty(ctx, project.ID); err != nil {
		t.Fatalf("ClearProjectStartedEmpty() error = %v", err)
	}

	got, err := store.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if got.StartedEmpty {
		t.Fatal("StartedEmpty = true after hand seeding; recovery would delete the seeded content")
	}
}

// Clearing is idempotent: a second clear must leave the bit false, and it must
// never set it on a project that did not start empty.
func TestClearProjectStartedEmptyIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p", StartEmptyWorkspace: true,
		Tasks: []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	for range 2 {
		if err := store.ClearProjectStartedEmpty(ctx, project.ID); err != nil {
			t.Fatalf("ClearProjectStartedEmpty() error = %v", err)
		}
	}
	got, err := store.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if got.StartedEmpty {
		t.Fatal("StartedEmpty = true after clearing a started-empty project twice")
	}

	// A project that never started empty must stay that way.
	plain, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "q",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	if err := store.ClearProjectStartedEmpty(ctx, plain.ID); err != nil {
		t.Fatalf("ClearProjectStartedEmpty() error = %v", err)
	}
	got, err = store.GetProject(ctx, plain.ID)
	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if got.StartedEmpty {
		t.Fatal("StartedEmpty = true, want false for a project that never started empty")
	}
}
