package worker

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestLegacyBreakdown_DepthCap_EmitsHandoff(t *testing.T) {
	t.Parallel()

	parent := models.Task{BaseEntity: models.BaseEntity{ID: "parent-1"}, Title: "parent"}
	grand := models.Task{BaseEntity: models.BaseEntity{ID: "grand-1"}, Title: "grand"}
	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-deep"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: false},
			splitParents: map[string][]models.Task{
				"task-deep": {parent},
				"parent-1":  {grand},
			},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"x","description":"y"}]}`}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.Process(context.Background(), store.task)

	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff when breakdown depth exceeds cap")
	}
}

func TestLegacyBreakdown_DepthCapIgnoresNonSplitAncestry(t *testing.T) {
	t.Parallel()

	dependency := models.Task{BaseEntity: models.BaseEntity{ID: "dependency-1"}, Title: "dependency"}
	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-dependent"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: false},
			parents: map[string][]models.Task{
				"task-dependent": {dependency},
			},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"}]}`}
	opts := legacyCapOptions()
	opts.Split.MaxDepth = 1
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, opts)
	w.Process(context.Background(), store.task)

	store.mu.Lock()
	subtasks := store.subtasks
	store.mu.Unlock()
	if len(subtasks) != 2 {
		t.Fatalf("subtasks = %d, want 2 (a non-split parent must not consume depth)", len(subtasks))
	}
}
