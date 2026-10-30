package agentic

import (
	"testing"

	"agentd/internal/models"
)

// exec.turn carries the task's attributes, so a requires_workspace_change rule
// on it can match. Without them every rule on the purpose is skipped.
func TestBuildAgenticRequestCarriesTaskAttrs(t *testing.T) {
	t.Parallel()
	e := &Engine{host: &noopHost{}}
	for _, flagged := range []bool{true, false} {
		task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, AgentID: "a1", RequiresWorkspaceChange: flagged}
		req := e.buildAgenticRequest(task, models.AgentProfile{}, nil, nil, 0)
		got := req.Attrs.RequiresWorkspaceChange
		if got == nil || *got != flagged {
			t.Fatalf("flagged=%v: Attrs.RequiresWorkspaceChange = %v, want %v", flagged, got, flagged)
		}
	}
}
