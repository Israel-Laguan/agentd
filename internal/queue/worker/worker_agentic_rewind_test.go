package worker

import (
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestAgenticRewindState_Stagnation(t *testing.T) {
	t.Parallel()
	s := &agenticRewindState{}
	for i := 0; i < maxRewindStreak; i++ {
		if s.apply(rewindToFirstTurn) {
			t.Fatalf("stagnation on apply %d, want only after streak > %d", i+1, maxRewindStreak)
		}
	}
	if !s.apply(rewindToFirstTurn) {
		t.Fatalf("expected stagnation after %d rewinds to same target", maxRewindStreak+1)
	}
}

func TestAgenticRewindState_DifferentTargetResetsStreak(t *testing.T) {
	t.Parallel()
	s := &agenticRewindState{}
	s.apply(rewindToFirstTurn)
	s.apply(rewindToFirstTurn)
	if s.apply(1) {
		t.Fatal("different rewind target should not stagnate on first apply")
	}
	if s.streak != 1 {
		t.Fatalf("streak = %d, want 1 after target change", s.streak)
	}
}

func TestAgenticRewindState_ForwardProgressResetsStreak(t *testing.T) {
	t.Parallel()
	s := &agenticRewindState{}
	s.apply(rewindToFirstTurn)
	s.apply(rewindToFirstTurn)
	if s.streak != 2 {
		t.Fatalf("streak = %d, want 2 before forward progress reset", s.streak)
	}
	s.reset()
	if s.apply(rewindToFirstTurn) {
		t.Fatal("forward progress reset should clear streak; same target after reset must not stagnate")
	}
	if s.streak != 1 {
		t.Fatalf("streak = %d, want 1 after reset and re-apply", s.streak)
	}
}

func TestSessionRecoveryPlanReinjection_ClearsFlagAfterInject(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{})
	plan := &Plan{Steps: []PlanStep{{ID: "only", Action: "do", OutputFormat: "text"}}}
	msgs := []gateway.PromptMessage{{Role: "system", Content: "base"}}
	in := agenticTurnLoopInput{
		sessionRecoveryUsed: true,
		workPlan:            plan,
		messages:            &msgs,
	}
	rewindInjectPlan := func() {
		if in.sessionRecoveryUsed && in.workPlan != nil && in.messages != nil {
			*in.messages = w.injectPlan(*in.messages, in.workPlan)
			in.sessionRecoveryUsed = false
		}
	}
	rewindInjectPlan()
	if strings.Count((*in.messages)[0].Content, "WORK PLAN") != 1 {
		t.Fatalf("first reinject should leave one WORK PLAN block, got %q", (*in.messages)[0].Content)
	}
	rewindInjectPlan()
	if strings.Count((*in.messages)[0].Content, "WORK PLAN") != 1 {
		t.Fatal("cleared sessionRecoveryUsed must prevent duplicate plan reinjection on later rewind")
	}
}

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
