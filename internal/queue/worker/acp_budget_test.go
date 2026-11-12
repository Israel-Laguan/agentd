package worker

import (
	"context"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/queue/runaudit"

	acpsdk "github.com/coder/acp-go-sdk"
)

// exec.work_plan can route to an ACP agent. The response is charged to the
// caller's attempt budget once: ACP never runs the correction loop, so the call
// must not also charge its own guard and GeneratePlan's fallback.
func TestACPWorkPlanChargesTheBudgetOnce(t *testing.T) {
	t.Parallel()
	step := report(map[string]any{"steps": []any{map[string]any{
		"id": "analyze", "action": "read the code",
	}}})
	step.Usage = &acpsdk.Usage{TotalTokens: 321}
	w, _, task, project, _, _, _ := acpFixture(t, "plan the work", false)
	task.Description = fakeagent.Script(step)
	w.routes.Stages[spec.PurposeExecWorkPlan] = route.StageRoute{Default: "coder"}
	tracker := gateway.NewBudgetTracker(10000)
	guard := runaudit.NewBudgetGuard(tracker, task.ID)

	plan, err := w.GeneratePlan(context.Background(), task, project, guard)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "analyze" {
		t.Fatalf("plan = %+v, want the one analyze step", plan)
	}
	if got := guard.Usage(); got != 321 {
		t.Fatalf("budget usage = %d, want 321: the ACP response must be charged once", got)
	}
}

type refusingBudgetTracker struct{ reserves int }

func (b *refusingBudgetTracker) Reserve(string) error {
	b.reserves++
	return models.ErrBudgetExceeded
}
func (b *refusingBudgetTracker) Add(string, int)  {}
func (b *refusingBudgetTracker) Usage(string) int { return 0 }
func (b *refusingBudgetTracker) Reset(string)     {}

// An ACP exec.turn reserves the task token budget before the agent starts.
func TestACPExecutionReservesTaskBudget(t *testing.T) {
	t.Parallel()
	script := fakeagent.Script(report(map[string]any{"summary": "summary"}))
	w, store, task, project, profile, sink, _ := acpFixture(t, script, false)
	tracker := &refusingBudgetTracker{}
	w.budgetTracker = tracker

	w.dispatchProcess(context.Background(), task, project, profile)

	if tracker.reserves == 0 {
		t.Fatal("task budget was never reserved for the ACP call")
	}
	got, err := store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State == models.TaskStateCompleted {
		t.Fatal("task completed despite an exhausted task token budget")
	}
	if recs := sink.all(); len(recs) != 0 {
		t.Fatalf("acp call records = %+v, want none: the agent must not start over budget", recs)
	}
}
