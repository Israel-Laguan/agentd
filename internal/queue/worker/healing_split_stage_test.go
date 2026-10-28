package worker

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/stage"
)

func TestHealingSplitMakesOneTaskCreationCall(t *testing.T) {
	t.Parallel()
	store := healingSplitStore("task-split-stage")
	store.profile.Temperature = 0.4
	store.profile.MaxTokens = 900
	agent := &stageRecorder{output: `{"subtasks":[{"title":"a","description":"1"}]}`}
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.stageAgent = agent

	w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

	if len(agent.reqs) != 1 {
		t.Fatalf("agent calls = %d, want exactly 1", len(agent.reqs))
	}
	req := agent.reqs[0]
	if req.Stage != stage.StageTaskCreation || req.Purpose != gateway.PurposeCreateSplitTask {
		t.Errorf("call = stage %q purpose %q, want task creation and split task", req.Stage, req.Purpose)
	}
	if req.Role != gateway.RoleWorker || req.AgentID != "agent-1" {
		t.Errorf("role %q agent %q, want worker and the task's agent", req.Role, req.AgentID)
	}
	if req.Temperature != 0.4 || req.MaxTokens != 900 || req.Provider != "ollama" || req.Model != "llama3" {
		t.Errorf("temperature %v max %d model %s/%s, want the profile's", req.Temperature, req.MaxTokens, req.Provider, req.Model)
	}
	if agent.taskIDs[0] != "task-split-stage" {
		t.Errorf("task id in ctx = %q, want the split task", agent.taskIDs[0])
	}
}

func TestHealingSplitRecordsTokensOfAFailedCall(t *testing.T) {
	t.Parallel()
	store := healingSplitStore("task-split-fail")
	ts := &fakeTokenStore{}
	opts := legacyCapOptions()
	opts.TokenStore = ts
	agent := &stageRecorder{err: errors.New("invalid json"), tokens: 42}
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, opts)
	w.stageAgent = agent

	w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

	if got := ts.totalTokens(); got != 42 {
		t.Fatalf("recorded tokens = %d, want 42: a failed split still cost them", got)
	}
}

// The split contract has no flag, so the children take the parent's: a flagged
// task that is split keeps the evidence gate on the work.
func TestHealingSplitChildrenInheritRequiresWorkspaceChange(t *testing.T) {
	t.Parallel()
	for _, flagged := range []bool{true, false} {
		store := healingSplitStore("task-split-flag")
		store.task.RequiresWorkspaceChange = flagged
		agent := &stageRecorder{output: `{"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"}]}`}
		w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
		w.stageAgent = agent

		w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

		store.mu.Lock()
		if len(store.subtasks) != 2 {
			t.Fatalf("flagged=%v: subtasks = %d, want 2", flagged, len(store.subtasks))
		}
		for _, d := range store.subtasks {
			if d.RequiresWorkspaceChange != flagged {
				t.Errorf("parent flagged=%v: child %q flagged=%v", flagged, d.Title, d.RequiresWorkspaceChange)
			}
		}
		store.mu.Unlock()
	}
}
