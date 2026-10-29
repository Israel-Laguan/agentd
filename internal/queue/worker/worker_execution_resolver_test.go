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
			want:    executionAgentReview,
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
			name:   "phase planning",
			worker: &Worker{store: store},
			task:   models.Task{Title: "plan phase 2"},
			want:   executionAgentPhasePlanning,
		},
		{
			name:   "tiered origin",
			worker: &Worker{store: store, tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 1}},
			task:   models.Task{Title: "implement a complex integration with many services and detailed validation requirements", Description: "configure the api database cache queue and deployment pipeline including tests and documentation"},
			want:   executionAgentTieredOrigin,
		},
		{
			name:   "tiered step",
			worker: &Worker{store: store},
			task:   models.Task{AgentID: "tier-execute"},
			want:   executionAgentTieredStep,
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
			want:   executionAgentAgentic,
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
