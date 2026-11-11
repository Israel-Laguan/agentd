package worker

import (
	"context"
	"errors"
	"log/slog"

	"agentd/internal/models"
	"agentd/internal/queue/hostreport"
	"agentd/internal/sandbox"
)

// tryRenderHostReport completes a host-fact report task by rendering the
// summary from the files earlier tasks collected, instead of asking the model
// for a shell command that would paste raw output (B-045). Code writes the
// report, so every value in it comes from a collected file and machine or boot
// identifiers are never read. It returns false, leaving the task untouched,
// when the task is not a report task, when no host facts were collected, or
// when the collection is partial: retrying the render on the same workspace
// cannot produce the missing facts (the collectors already ran), and no ACP
// route can either, so the task fails with the render reason on its event log
// rather than burning the retry budget on an unchanged workspace (B-046).
func (w *Worker) tryRenderHostReport(ctx context.Context, task models.Task, project models.Project) bool {
	if w.goldenRepairPending(ctx, task) {
		return false
	}
	target, ok := hostreport.ReportTarget(task.Title, task.Description)
	if !ok || w.validateProjectWorkspace(project) != nil {
		return false
	}

	report, ok, err := hostreport.WriteReport(project.WorkspacePath, target)
	if err != nil {
		var missing *hostreport.MissingFactsError
		if errors.As(err, &missing) {
			slog.Warn("host report incomplete; execution requires an ACP route", "task_id", task.ID, "error", err)
			w.Emit(ctx, task, "HOST_REPORT_INCOMPLETE", err.Error())
			return false
		}
		slog.Warn("host report render failed; execution requires an ACP route", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "HOST_REPORT_RENDER_FAILED", err.Error())
		return false
	}
	if !ok {
		return false
	}
	w.Emit(ctx, task, "REPORT_RENDERED", "wrote "+target+" from collected host facts")
	if len(task.SuccessCriteria) > 0 {
		if err := verifyPostConditions(task, project.WorkspacePath); err != nil {
			w.handleAgentFailure(ctx, task, err.Error())
			return true
		}

	}
	w.commit(ctx, task, sandbox.Result{Success: true, Stdout: report}, nil)
	return true
}
