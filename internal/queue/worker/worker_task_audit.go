package worker

import (
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/models"
)

// taskAudit carries the outcome flags a task execution ends on: failed, a
// human review handoff, or a suspension awaiting that human's answer.
type taskAudit struct {
	failed        bool
	reviewHandoff bool
	suspended     bool
}

// beginTaskAudit starts one task audit run and returns its outcome flags.
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

// finishTaskAudit records the task's closing outcome: a completion, a failure,
// a human review handoff, or a suspension awaiting a human's answer.
func (w *Worker) finishTaskAudit(task models.Task, project models.Project, provider string, audit *taskAudit) {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return
	}
	recType := agentruntime.RecordTypeTaskComplete
	switch {
	case audit.suspended:
		recType = agentruntime.RecordTypeTaskSuspended
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
