package worker

import (
	"context"
	"fmt"
	"log/slog"

	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/models"
	"agentd/internal/queue/hostreport"
	"agentd/internal/queue/planning"
)

type executionAgent string

const (
	executionAgentRollup        executionAgent = "builtin-rollup"
	executionAgentPhasePlanning executionAgent = "builtin-phase-planning"
	executionAgentHostReport    executionAgent = "builtin-host-report"
	executionAgentACP           executionAgent = "acp"
	executionAgentMissingRoute  executionAgent = "missing-exec-route"
)

type executionResolution struct {
	kind executionAgent
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
		resolved = w.resolveExecutionRoute(task)
	case executionAgentPhasePlanning:
		slog.DebugContext(ctx, "worker: handling phase planning", "task_id", task.ID)
		w.handlePhasePlanning(ctx, task, project)
		return
	}
	if resolved.kind == executionAgentACP {
		w.runACPExecution(ctx, task, project, profile, resolved)
		return
	}
	if resolved.kind == executionAgentHostReport && w.tryRenderHostReport(ctx, task, project) {
		return
	}
	w.failTerminal(ctx, task, fmt.Errorf("no ACP route for exec.turn (task agent %q)", task.AgentID), models.TaskStateFailedRequiresHuman)
}

func (w *Worker) resolveForGoldenRepair(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) executionResolution {
	resolved := w.resolveExecutionAgent(ctx, task, project, profile)
	if !w.goldenRepairPending(ctx, task) || (resolved.kind != executionAgentRollup && resolved.kind != executionAgentHostReport) {
		return resolved
	}
	return w.resolveExecutionRoute(task)
}

func (w *Worker) resolveExecutionAgent(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) executionResolution {
	// A review-required task must go through createReviewHandoff, not the
	// roll-up: tryRollUpBreakdown commits the parent directly, which would
	// complete it with no human review (T-068).
	if profile.RequireReview {
		return w.resolveExecutionRoute(task)
	}
	if w.hasBreakdownToRollUp(ctx, task) {
		return executionResolution{kind: executionAgentRollup}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		return executionResolution{kind: executionAgentPhasePlanning}
	}
	// An external agent runs the task whole: no tiered split, no host-report shortcut.
	resolved := w.resolveExecutionRoute(task)
	if resolved.kind == executionAgentACP {
		return resolved
	}
	if _, ok := hostreport.ReportTarget(task.Title, task.Description); ok {
		return executionResolution{kind: executionAgentHostReport}
	}
	return resolved
}

func (w *Worker) resolveExecutionRoute(task models.Task) executionResolution {
	if d, a, ok := w.acpRoute(gateway.PurposeExecTurn, task.AgentID, gateway.AttrsForTask(task.RequiresWorkspaceChange)); ok {
		return executionResolution{kind: executionAgentACP, route: d, agent: a}
	}
	return executionResolution{kind: executionAgentMissingRoute}
}

func (w *Worker) hasBreakdownToRollUp(ctx context.Context, task models.Task) bool {
	comments, err := w.store.ListComments(ctx, task.ID)
	if err != nil {
		return true
	}
	_, marked := latestBreakdownSubtaskCount(comments)
	return marked
}
