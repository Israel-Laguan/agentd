package worker

import (
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/models"
)

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
