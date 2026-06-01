package agentic

import (
	"strings"
	"testing"

	agentcontext "agentd/internal/agent/context"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

type rewindMockHost struct {
	*noopHost
}

func (h *rewindMockHost) InjectPlan(messages []gateway.PromptMessage, plan *agentcontext.Plan) []gateway.PromptMessage {
	if len(messages) == 0 {
		return messages
	}
	messages[0].Content += "\nWORK PLAN"
	return messages
}

func TestSessionRecoveryPlanReinjection_ClearsNeedsPlanInjectAfterInject(t *testing.T) {
	t.Parallel()
	host := &rewindMockHost{noopHost: &noopHost{}}
	e := &Engine{host: host}
	plan := &agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: "only", Action: "do", OutputFormat: "text"}}}
	msgs := []gateway.PromptMessage{{Role: "system", Content: "base"}}
	in := agenticTurnLoopInput{
		sessionRecoveryUsed:            true,
		sessionRecoveryNeedsPlanInject: true,
		workPlan:                       plan,
		messages:                       &msgs,
	}
	e.applySessionRecoveryPlanReinjection(&in)
	if strings.Count((*in.messages)[0].Content, "WORK PLAN") != 1 {
		t.Fatalf("first reinject should leave one WORK PLAN block, got %q", (*in.messages)[0].Content)
	}
	e.applySessionRecoveryPlanReinjection(&in)
	if strings.Count((*in.messages)[0].Content, "WORK PLAN") != 1 {
		t.Fatal("cleared sessionRecoveryNeedsPlanInject must prevent duplicate plan reinjection on later rewind")
	}
	if !in.sessionRecoveryUsed {
		t.Fatal("sessionRecoveryUsed must stay true after plan reinjection")
	}
}

func TestResetAgenticStateForTopicDrift_ReplacesContextManager(t *testing.T) {
	t.Parallel()
	e := &Engine{}
	oldCM := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent-1", "task-1")
	in := agenticTurnLoopInput{
		task: models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}, AgentID: "agent-1"},
		cm:   oldCM,
	}
	resetAgenticStateForTopicDrift(&in, e)
	if in.cm == nil {
		t.Fatal("expected non-nil context manager after topic drift reset")
	}
	if in.cm == oldCM {
		t.Fatal("expected fresh context manager after topic drift reset")
	}
}
