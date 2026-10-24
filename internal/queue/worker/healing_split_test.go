package worker

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func healingSplitStore(id string) *legacyHandoffStore {
	return &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity:  models.BaseEntity{ID: id},
				ProjectID:   "project-1",
				AgentID:     "agent-1",
				Title:       "Build the report",
				Description: "collect host facts",
				State:       models.TaskStateQueued,
				RetryCount:  3,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{
				ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: true,
				SystemPrompt: sql.NullString{String: "You are an agent with tools.", Valid: true},
			},
		},
	}
}

// The healing split is its own task-creation stage: it no longer goes through
// the legacy shell-command prompt, so deleting the legacy engine (T-055b)
// leaves it working.
func TestHealingSplitRunsAsItsOwnStage(t *testing.T) {
	t.Parallel()

	store := healingSplitStore("task-split")
	gw := &legacyContentGateway{content: `{"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"}]}`}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())

	w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

	if len(gw.requests) == 0 {
		t.Fatal("no model call made")
	}
	req := gw.requests[0]
	if req.Purpose != gateway.PurposeCreateSplitTask || req.TaskID != "task-split" {
		t.Errorf("request purpose %q task %q, want %q task-split", req.Purpose, req.TaskID, gateway.PurposeCreateSplitTask)
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, legacyJSONCommandSystemSentinel) {
			t.Errorf("split call carries the legacy command prompt:\n%s", m.Content)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.subtasks) != 2 {
		t.Fatalf("subtasks = %d, want 2", len(store.subtasks))
	}
}

func TestHealingSplitWithNoSubtasksHandsOff(t *testing.T) {
	t.Parallel()

	store := healingSplitStore("task-unsplittable")
	sink := &legacyHandoffSink{}
	gw := &legacyContentGateway{content: `{"subtasks":[]}`}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, sink, legacyCapOptions())

	w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if !blocked || !containsEvent(sink.kinds, "HEALING_HANDOFF") {
		t.Fatalf("blocked=%v events=%v, want a HEALING_HANDOFF", blocked, sink.kinds)
	}
}

func TestHandleHealingSplit_SubtaskCap_EmitsHealingHandoff(t *testing.T) {
	t.Parallel()

	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-heal-cap"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
				RetryCount: 3,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{
				ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: true,
			},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"},{"title":"c","description":"3"},{"title":"d","description":"4"},{"title":"e","description":"5"},{"title":"f","description":"6"}]}`}
	sink := &legacyHandoffSink{}
	opts := legacyCapOptions()
	opts.Split.MaxSubtasks = 5
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, sink, opts)

	w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff when healing split exceeds subtask cap")
	}
	sink.mu.Lock()
	kinds := sink.kinds
	sink.mu.Unlock()
	if !containsEvent(kinds, "HEALING_HANDOFF") {
		t.Fatalf("expected HEALING_HANDOFF event; got %v", kinds)
	}
	if containsEvent(kinds, "LEGACY_MODE_HANDOFF") {
		t.Fatalf("unexpected LEGACY_MODE_HANDOFF event; got %v", kinds)
	}
}
