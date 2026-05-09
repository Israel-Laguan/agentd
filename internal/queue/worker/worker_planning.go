package worker

import (
	"context"
	"fmt"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

func (w *Worker) handleTaskBreakdown(ctx context.Context, task models.Task, subtasks []workerSubtask) {
	drafts := make([]models.DraftTask, 0, len(subtasks))
	for _, subtask := range subtasks {
		drafts = append(drafts, models.DraftTask{
			Title:       subtask.Title,
			Description: subtask.Description,
			Assignee:    models.TaskAssigneeSystem,
		})
	}
	if len(drafts) == 0 {
		w.handleAgentFailure(ctx, task, "worker reported task too complex without subtasks")
		return
	}
	if _, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, drafts); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "TASK_BREAKDOWN", fmt.Sprintf("created %d subtasks", len(drafts)))
}

func (w *Worker) handlePhasePlanning(ctx context.Context, task models.Task, project models.Project) {
	tasks, err := w.store.ListTasksByProject(ctx, project.ID)
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	intent := planning.BuildPhaseIntent(task, project, tasks)
	plan, err := w.gateway.GeneratePlan(ctx, intent)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return
	}
	plan.Tasks = planning.RetitlePhaseContinuationTasks(plan.Tasks, planning.NextPhaseNumber(task.Title))
	created, err := w.store.AppendTasksToProject(ctx, project.ID, task.ID, plan.Tasks)
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if _, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: true,
		Payload: fmt.Sprintf("planned next phase with %d tasks", len(created)),
	}); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "PHASE_PLANNING", fmt.Sprintf("created %d phase tasks", len(created)))
}
