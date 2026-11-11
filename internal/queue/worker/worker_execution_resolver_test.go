package worker

import (
	"context"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestResolveExecutionAgent(t *testing.T) {
	store := &testutil.FakeKanbanStore{}
	tests := resolverCases(store)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.worker.resolveExecutionAgent(context.Background(), tt.task, models.Project{}, tt.profile)
			if got.kind != tt.want {
				t.Fatalf("resolved agent = %q, want %q", got.kind, tt.want)
			}
		})
	}
}

type resolverCase struct {
	name    string
	worker  *Worker
	task    models.Task
	profile models.AgentProfile
	want    executionAgent
}

func resolverCases(store *testutil.FakeKanbanStore) []resolverCase {
	return []resolverCase{
		{
			name:    "review",
			worker:  &Worker{store: store},
			profile: models.AgentProfile{RequireReview: true},
			want:    executionAgentMissingRoute,
		},
		{
			name: "rollup",
			worker: &Worker{store: &resolverRollupStore{
				FakeKanbanStore: store,
				comments:        []models.Comment{breakdownMarker(1)},
			}},
			want: executionAgentRollup,
		},
		{
			// A review-required task must not take the roll-up shortcut: the
			// roll-up commits the parent directly, with no review handoff.
			name:    "review required skips rollup",
			worker:  &Worker{store: &resolverRollupStore{FakeKanbanStore: store, comments: []models.Comment{breakdownMarker(1)}}},
			profile: models.AgentProfile{RequireReview: true},
			want:    executionAgentMissingRoute,
		},
		{
			// Phase planning is a builtin step, not an exec turn, so a
			// review-required profile keeps it instead of routing it to ACP.
			name:    "review required keeps phase planning",
			worker:  &Worker{store: store},
			task:    models.Task{Title: "plan phase 2"},
			profile: models.AgentProfile{RequireReview: true},
			want:    executionAgentPhasePlanning,
		},
		{
			name:   "phase planning",
			worker: &Worker{store: store},
			task:   models.Task{Title: "plan phase 2"},
			want:   executionAgentPhasePlanning,
		},
		{
			name:   "tiered origin",
			worker: &Worker{store: store, tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 1}},
			task:   models.Task{Title: "implement a complex integration with many services and detailed validation requirements", Description: "configure the api database cache queue and deployment pipeline including tests and documentation"},
			want:   executionAgentMissingRoute,
		},
		{
			name:   "tiered step",
			worker: &Worker{store: store},
			task:   models.Task{AgentID: "tier-execute"},
			want:   executionAgentMissingRoute,
		},
		{
			name:   "host report",
			worker: &Worker{store: store},
			task:   models.Task{Title: models.HostReportTaskTitle, Description: "Create system report.md summarizing the OS and hardware details from os_info.txt"},
			want:   executionAgentHostReport,
		},
		{
			name:   "agentic",
			worker: &Worker{store: store},
			task:   models.Task{Title: "simple task"},
			want:   executionAgentMissingRoute,
		},
	}
}

type resolverRollupStore struct {
	*testutil.FakeKanbanStore
	comments []models.Comment
}

func (s *resolverRollupStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return s.comments, nil
}
