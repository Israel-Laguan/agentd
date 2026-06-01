package worker

import (
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestSessionRecoveryPlanReinjection_ClearsNeedsPlanInjectAfterInject(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{})
	plan := &Plan{Steps: []PlanStep{{ID: "only", Action: "do", OutputFormat: "text"}}}
	msgs := []gateway.PromptMessage{{Role: "system", Content: "base"}}
	in := agenticTurnLoopInput{
		sessionRecoveryUsed:            true,
		sessionRecoveryNeedsPlanInject: true,
		workPlan:                       plan,
		messages:                       &msgs,
	}
	w.applySessionRecoveryPlanReinjection(&in)
	if strings.Count((*in.messages)[0].Content, "WORK PLAN") != 1 {
		t.Fatalf("first reinject should leave one WORK PLAN block, got %q", (*in.messages)[0].Content)
	}
	w.applySessionRecoveryPlanReinjection(&in)
	if strings.Count((*in.messages)[0].Content, "WORK PLAN") != 1 {
		t.Fatal("cleared sessionRecoveryNeedsPlanInject must prevent duplicate plan reinjection on later rewind")
	}
	if !in.sessionRecoveryUsed {
		t.Fatal("sessionRecoveryUsed must stay true after plan reinjection")
	}
}

func TestResetAgenticStateForTopicDrift_ReplacesContextManager(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{})
	oldCM := NewContextManager(config.AgenticContextConfig{}, nil, "agent-1", "task-1")
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
