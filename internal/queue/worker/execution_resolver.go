package worker

import (
	"context"
	"log/slog"

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
)

type executionResolution struct {
	kind executionAgent
}

func (w *Worker) dispatchProcess(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	resolved := w.resolveExecutionAgent(ctx, task, project, profile)
	switch resolved.kind {
	case executionAgentReview:
		done, err := w.tryFinalizeApprovedReview(ctx, task)
		if err != nil {
			w.FailHard(ctx, task, err)
			return
		}
		if done {
			return
		}
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
		if w.tryTieredOrigin(ctx, task) {
			return
		}
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

func (w *Worker) resolveExecutionAgent(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) executionResolution {
	if profile.RequireReview {
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
	if steps, err := w.tieredStepChildren(ctx, task); err == nil && len(steps) > 0 {
		return executionResolution{kind: executionAgentTieredOrigin}
	}
	if w.ShouldRunTiered(task) {
		return executionResolution{kind: executionAgentTieredOrigin}
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
