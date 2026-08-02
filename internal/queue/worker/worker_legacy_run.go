package worker

import (
	"context"
	"errors"

	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func (w *Worker) runLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, profileAlreadyRouted bool) {
	if !profileAlreadyRouted {
		profile = w.routeLegacyProfile(ctx, task, project, profile)
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
	w.recordTaskTokenUsage(ctx, task, tokenUsage, details)
	audit.command = response.Command
	audit.tokenUsage = tokenUsage
	if err != nil {
		audit.failed = true
		if errors.Is(err, models.ErrInvalidJSONResponse) {
			w.createLegacyModeHandoff(ctx, task, "Gateway could not return valid JSON after repair attempts.", err)
			return workerResponse{}, false
		}
		w.handleGatewayError(ctx, task, err)
		return workerResponse{}, false
	}
	if response.TooComplex {
		audit.failed = true
		w.handleLegacyTaskBreakdown(ctx, task, response.Subtasks, false)
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
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.registerCancel(task.ID, cancel)
	defer w.deregisterCancel(task.ID)

	result, runErr := w.sandbox.Execute(execCtx, w.payload(task, project, command))
	audit.exitCode = result.ExitCode
	if runErr != nil || !result.Success {
		audit.failed = true
	}
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, project, command, result)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, command, result)
		return
	}
	if profile.RequireReview && runErr == nil && result.Success {
		audit.reviewHandoff = true
		w.createReviewHandoff(ctx, task, result.Stdout)
		return
	}
	w.commit(ctx, task, result, runErr)
}

func (w *Worker) commitTextWithProfile(ctx context.Context, task models.Task, content string, profile *models.AgentProfile) {
	if profile != nil && profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.emit(ctx, task, "ERROR", err.Error())
			w.failHard(ctx, task, err)
			return
		} else if done {
			return
		}
		w.createReviewHandoff(ctx, task, content)
		return
	}
	result := sandbox.Result{
		Success: true,
		Stdout:  content,
	}
	w.commit(ctx, task, result, nil)
}

type legacyTaskAudit struct {
	command       string
	exitCode      int
	tokenUsage    int
	failed        bool
	reviewHandoff bool
}

func (w *Worker) beginLegacyTaskAudit(task models.Task, project models.Project, provider string) *legacyTaskAudit {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return &legacyTaskAudit{}
	}
	w.auditLogger.RecordTaskEvent(agentruntime.TaskAuditRecord{
		RecordType: agentruntime.RecordTypeTaskStart,
		TaskID:     task.ID,
		ProjectID:  project.ID,
		Provider:   provider,
	})
	return &legacyTaskAudit{}
}

func (w *Worker) finishLegacyTaskAudit(task models.Task, project models.Project, provider string, audit *legacyTaskAudit) {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return
	}
	recType := agentruntime.RecordTypeTaskComplete
	switch {
	case audit.failed:
		recType = agentruntime.RecordTypeTaskFail
	case audit.reviewHandoff:
		recType = agentruntime.RecordTypeTaskReview
	}
	w.auditLogger.RecordTaskEvent(agentruntime.TaskAuditRecord{
		RecordType: recType,
		TaskID:     task.ID,
		ProjectID:  project.ID,
		Provider:   provider,
		Command:    audit.command,
		ExitCode:   audit.exitCode,
		TokenUsage: audit.tokenUsage,
	})
}
