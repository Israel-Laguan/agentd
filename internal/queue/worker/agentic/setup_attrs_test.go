package agentic

import (
	"context"
	"testing"

	"agentd/internal/models"
)

// prepareAgenticRun carries the task's routing attributes onto the tool
// executor, so nested subagent turns can match a requires_workspace_change
// rule. Removing the WithAttrs wiring must fail this test.
func TestPrepareAgenticRunCarriesTaskAttrs(t *testing.T) {
	t.Parallel()
	e := &Engine{host: &noopHost{}}
	for _, flagged := range []bool{true, false} {
		task := models.Task{
			BaseEntity:              models.BaseEntity{ID: "t1"},
			ProjectID:               "p1",
			AgentID:                 "a1",
			Title:                   "title",
			Description:             "description",
			RequiresWorkspaceChange: flagged,
		}
		project := models.Project{BaseEntity: models.BaseEntity{ID: "p1"}}
		_, _, _, _, _, _, exec := e.prepareAgenticRun(context.Background(), task, project, models.AgentProfile{}, nil)
		got := exec.Attrs().RequiresWorkspaceChange
		if got == nil || *got != flagged {
			t.Fatalf("flagged=%v: executor Attrs.RequiresWorkspaceChange = %v, want %v", flagged, got, flagged)
		}
	}
}
