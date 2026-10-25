package worker

import (
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/models"
)

type taskAudit struct {
	failed        bool
	reviewHandoff bool
}

func (w *Worker) beginTaskAudit(task models.Task, project models.Project, provider string) *taskAudit {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return &taskAudit{}
	}
	w.auditLogger.RecordTaskEvent(agentruntime.TaskAuditRecord{
		RecordType: agentruntime.RecordTypeTaskStart,
		TaskID:     task.ID,
		ProjectID:  project.ID,
		Provider:   provider,
	})
	return &taskAudit{}
}

func (w *Worker) finishTaskAudit(task models.Task, project models.Project, provider string, audit *taskAudit) {
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
	})
}
