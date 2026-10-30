package worker

import (
	"context"
	"errors"
	"testing"

	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/stage"
)

const oneStepPlan = `{"steps":[{"id":"analyze","action":"read the code","output_format":"text"}]}`

func workPlanTask() models.Task {
	return models.Task{BaseEntity: models.BaseEntity{ID: "t-plan"}, AgentID: "agent-1", Title: "x", Description: "do it"}
}

func TestWorkPlanMakesOneTaskCreationCall(t *testing.T) {
	t.Parallel()
	agent := &stageRecorder{output: oneStepPlan}
	w := &Worker{stageAgent: agent}

	plan, err := w.GeneratePlan(context.Background(), workPlanTask(), models.Project{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "analyze" {
		t.Fatalf("plan = %+v, want the one analyze step", plan)
	}
	if len(agent.reqs) != 1 {
		t.Fatalf("agent calls = %d, want exactly 1", len(agent.reqs))
	}
	req := agent.reqs[0]
	if req.Stage != stage.StageTaskCreation || req.Purpose != gateway.PurposeExecWorkPlan {
		t.Errorf("call = stage %q purpose %q, want task creation and work plan", req.Stage, req.Purpose)
	}
	if req.MaxTokens != planPhaseTokenReserve || req.AgentID != "agent-1" {
		t.Errorf("max %d agent %q, want %d and the task's agent", req.MaxTokens, req.AgentID, planPhaseTokenReserve)
	}
	if agent.taskIDs[0] != "t-plan" {
		t.Errorf("task id in ctx = %q, want t-plan", agent.taskIDs[0])
	}
}

// The plan charges the task's budget guard but is not persisted as task usage.
func TestWorkPlanChargesTheBudgetGuardAndPersistsNothing(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}
	tracker := gateway.NewBudgetTracker(10000)
	guard := agentruntime.NewBudgetGuard(tracker, "t-plan")
	agent := &stageRecorder{output: oneStepPlan, tokens: 321}
	w := &Worker{stageAgent: agent, tokenStore: ts}

	if _, err := w.GeneratePlan(context.Background(), workPlanTask(), models.Project{}, guard); err != nil {
		t.Fatal(err)
	}
	if guard.Usage() != 321 {
		t.Errorf("budget usage = %d, want 321", guard.Usage())
	}
	if ts.callCount() != 0 {
		t.Errorf("token store calls = %d, want 0", ts.callCount())
	}
}

func TestWorkPlanFailedCallStillChargesTheBudgetGuard(t *testing.T) {
	t.Parallel()
	tracker := gateway.NewBudgetTracker(10000)
	guard := agentruntime.NewBudgetGuard(tracker, "t-plan")
	agent := &stageRecorder{err: errors.New("invalid json"), tokens: 90}
	w := &Worker{stageAgent: agent}

	if _, err := w.GeneratePlan(context.Background(), workPlanTask(), models.Project{}, guard); err == nil {
		t.Fatal("want the agent's error")
	}
	if guard.Usage() != 90 {
		t.Errorf("budget usage = %d, want 90: the failed attempts cost tokens", guard.Usage())
	}
}

func TestWorkPlanOverBudgetMakesNoCall(t *testing.T) {
	t.Parallel()
	tracker := gateway.NewBudgetTracker(100)
	tracker.Add("t-plan", 100)
	guard := agentruntime.NewBudgetGuard(tracker, "t-plan")
	agent := &stageRecorder{output: oneStepPlan}
	w := &Worker{stageAgent: agent}

	_, err := w.GeneratePlan(context.Background(), workPlanTask(), models.Project{}, guard)
	if !errors.Is(err, models.ErrBudgetExceeded) {
		t.Fatalf("error = %v, want ErrBudgetExceeded", err)
	}
	if len(agent.reqs) != 0 {
		t.Fatalf("agent calls = %d, want 0", len(agent.reqs))
	}
}
