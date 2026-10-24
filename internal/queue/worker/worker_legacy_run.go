package worker

import (
	"context"
	"errors"

	"agentd/internal/models"
)

func (w *Worker) RunLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, profileAlreadyRouted bool) {
	if !profileAlreadyRouted {
		profile = w.routeLegacyProfile(ctx, task, project, profile)
	}

	if w.tryRenderHostReport(ctx, task, project) {
		return
	}

	audit := w.beginLegacyTaskAudit(task, project, profile.Provider)
	defer func() {
		if r := recover(); r != nil {
			audit.failed = true
			w.finishLegacyTaskAudit(task, project, profile.Provider, audit)
			panic(r)
		}
		w.finishLegacyTaskAudit(task, project, profile.Provider, audit)
	}()

	response, ok := w.prepareLegacyExecution(ctx, task, project, profile, audit)
	if !ok {
		return
	}
	w.executeLegacyCommand(ctx, task, project, profile, response.Command, audit)
}

func (w *Worker) prepareLegacyExecution(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	audit *legacyTaskAudit,
) (workerResponse, bool) {
	if reason := w.legacyDispatchRejectReason(task, profile); reason != "" {
		audit.failed = true
		w.createLegacyModeHandoff(ctx, task, reason, nil)
		return workerResponse{}, false
	}

	response, tokenUsage, details, err := w.command(ctx, task, project, profile)
	w.RecordTaskTokenUsage(ctx, task, tokenUsage, details)
	audit.command = response.Command
	audit.tokenUsage = tokenUsage
	if err != nil {
		audit.failed = true
		if errors.Is(err, models.ErrInvalidJSONResponse) {
			w.createLegacyModeHandoff(ctx, task, "Gateway could not return valid JSON after repair attempts.", err)
			return workerResponse{}, false
		}
		w.HandleGatewayError(ctx, task, err)
		return workerResponse{}, false
	}
	if response.TooComplex {
		audit.failed = true
		w.applyBreakdown(ctx, task, response.Subtasks, false)
		return workerResponse{}, false
	}

	return response, true
}

func (w *Worker) executeLegacyCommand(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	command string,
	audit *legacyTaskAudit,
) {
	// An empty model command can never succeed; fail it without touching the
	// sandbox (B-043: the S5 empty fourth attempt executed as
	// `RESULT command is required`). A retry that would send the exact
	// command that just failed is deterministic waste; hand it off early
	// instead of burning the full budget.
	if w.rejectUnrunnableCommand(ctx, task, command, audit) {
		return
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.RegisterCancel(task.ID, cancel)
	defer w.DeregisterCancel(task.ID)

	result, runErr := w.executeRecorded(execCtx, task, project, command)
	audit.exitCode = result.ExitCode
	if runErr != nil || !result.Success {
		audit.failed = true
	}
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, project, command, result)
		return
	}
	if w.isPrivilegeBlock(result, runErr) {
		w.recoverLegacyPrivilege(ctx, task, project, profile, command, audit)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, command, "The sandbox reported a permission failure; no non-privileged recovery was attempted.")
		return
	}
	if profile.RequireReview && runErr == nil && result.Success {
		audit.reviewHandoff = true
		w.createReviewHandoff(ctx, task, result.Stdout)
		return
	}
	// Exit 0 alone does not complete a task that declares success criteria:
	// verify the cheap post-conditions first (B-039). A zero-exit command
	// that breaks the workspace fails with retry feedback instead of
	// COMPLETING, so the damage surfaces here rather than in a downstream
	// build task.
	if runErr == nil && result.Success && len(task.SuccessCriteria) > 0 {
		if err := verifyPostConditions(task, project.WorkspacePath); err != nil {
			audit.failed = true
			w.handleAgentFailure(ctx, task, err.Error())
			return
		}
	}
	w.commit(ctx, task, result, runErr)
}
