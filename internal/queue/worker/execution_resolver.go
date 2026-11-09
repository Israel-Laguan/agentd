package worker

import (
	"context"
	"log/slog"

	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/models"
	"agentd/internal/queue/hostreport"
	"agentd/internal/queue/planning"
)

type executionAgent string

const (
	executionAgentReview        executionAgent = "builtin-review"
	executionAgentRollup        executionAgent = "builtin-rollup"
	executionAgentPhasePlanning executionAgent = "builtin-phase-planning"
	executionAgentTieredOrigin  executionAgent = "builtin-tiered-origin"
	executionAgentTieredStep    executionAgent = "builtin-tiered"
	executionAgentHostReport    executionAgent = "builtin-host-report"
	executionAgentAgentic       executionAgent = "builtin-agentic"
	executionAgentACP           executionAgent = "acp"
)

type executionResolution struct {
	kind             executionAgent
	tieredSteps      []models.Task
	tieredStepsKnown bool
	tieredStepsErr   error
	// route and agent are the exec.turn decision and its acp agent (executionAgentACP).
	route route.Decision
	agent route.Agent
}

func (w *Worker) dispatchProcess(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	defer w.recoverPanic(ctx, task)
	if profile.RequireReview {
		done, err := w.tryFinalizeApprovedReview(ctx, task)
		if err != nil {
			w.FailHard(ctx, task, err)
			return
		}
		if done {
			return
		}
	}
	resolved := w.resolveForGoldenRepair(ctx, task, project, profile)
	switch resolved.kind {
	case executionAgentRollup:
		if handled, err := w.tryRollUpBreakdown(ctx, task); err != nil {
			w.Emit(ctx, task, "BREAKDOWN_ROLLUP_ERROR", err.Error())
			w.requeue(ctx, task, err.Error())
			return
		} else if handled {
			return
		}
	case executionAgentPhasePlanning:
		slog.DebugContext(ctx, "worker: handling phase planning", "task_id", task.ID)
		w.handlePhasePlanning(ctx, task, project)
		return
	case executionAgentTieredOrigin:
		if w.tryTieredOrigin(ctx, task, resolved.tieredSteps, resolved.tieredStepsKnown, resolved.tieredStepsErr) {
			return
		}
	case executionAgentACP:
		w.runACPExecution(ctx, task, project, profile, resolved)
		return
	}
	release, ok := w.gateProvider(ctx, task, profile.Provider)
	if !ok {
		return
	}
	defer release()
	if w.dispatchTieredStep(ctx, task, project, profile) {
		return
	}
	if w.tryRenderHostReport(ctx, task, project) {
		return
	}
	slog.DebugContext(ctx, "worker: entering agentic loop", "task_id", task.ID)
	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
	}
}

func (w *Worker) resolveForGoldenRepair(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) executionResolution {
	resolved := w.resolveExecutionAgent(ctx, task, project, profile)
	if !w.goldenRepairPending(ctx, task) || (resolved.kind != executionAgentRollup && resolved.kind != executionAgentTieredOrigin && resolved.kind != executionAgentHostReport) {
		return resolved
	}
	if d, a, ok := w.acpRoute(gateway.PurposeExecTurn, task.AgentID, gateway.AttrsForTask(task.RequiresWorkspaceChange)); ok {
		return executionResolution{kind: executionAgentACP, route: d, agent: a}
	}
	return executionResolution{kind: executionAgentAgentic}
}

func (w *Worker) resolveExecutionAgent(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) executionResolution {
	if profile.RequireReview {
		// A tiered step keeps its own dispatch path, so only whole tasks go to ACP.
		if !w.isTieredStep(task) {
			if d, a, ok := w.acpRoute(gateway.PurposeExecTurn, task.AgentID, gateway.AttrsForTask(task.RequiresWorkspaceChange)); ok {
				return executionResolution{kind: executionAgentACP, route: d, agent: a}
			}
		}
		return executionResolution{kind: executionAgentReview}
	}
	if w.hasBreakdownToRollUp(ctx, task) {
		return executionResolution{kind: executionAgentRollup}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		return executionResolution{kind: executionAgentPhasePlanning}
	}
	if w.isTieredStep(task) {
		return executionResolution{kind: executionAgentTieredStep}
	}
	steps, err := w.tieredStepChildren(ctx, task)
	if err != nil {
		return executionResolution{kind: executionAgentTieredOrigin, tieredStepsErr: err}
	}
	if len(steps) > 0 {
		return executionResolution{kind: executionAgentTieredOrigin, tieredSteps: steps, tieredStepsKnown: true}
	}
	// An external agent runs the task whole: no tiered split, no host-report shortcut.
	if d, a, ok := w.acpRoute(gateway.PurposeExecTurn, task.AgentID, gateway.AttrsForTask(task.RequiresWorkspaceChange)); ok {
		return executionResolution{kind: executionAgentACP, route: d, agent: a}
	}
	if w.ShouldRunTiered(task) {
		return executionResolution{kind: executionAgentTieredOrigin, tieredStepsKnown: true}
	}
	if _, ok := hostreport.ReportTarget(task.Title, task.Description); ok {
		return executionResolution{kind: executionAgentHostReport}
	}
	return executionResolution{kind: executionAgentAgentic}
}

func (w *Worker) hasBreakdownToRollUp(ctx context.Context, task models.Task) bool {
	comments, err := w.store.ListComments(ctx, task.ID)
	if err != nil {
		return true
	}
	_, marked := latestBreakdownSubtaskCount(comments)
	return marked
}
