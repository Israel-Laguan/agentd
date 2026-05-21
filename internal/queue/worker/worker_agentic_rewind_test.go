package worker

import (
	"testing"

	"agentd/internal/models"
)

func TestResetAgenticStateForTopicDrift_ReplacesContextManager(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{})
	oldCM := &ContextManager{taskID: "task-1", agentID: "agent-1"}
	in := agenticTurnLoopInput{
		task: models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}, AgentID: "agent-1"},
		cm:   oldCM,
	}
	resetAgenticStateForTopicDrift(&in, w)
	if in.cm == nil {
		t.Fatal("expected non-nil context manager after topic drift reset")
	}
	if in.cm == oldCM {
		t.Fatal("expected fresh context manager after topic drift reset")
	}
}
