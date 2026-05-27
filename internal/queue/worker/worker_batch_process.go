package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

// ProcessBatch runs a batched LLM call for same-context tasks, with per-slot fallback.
func (w *Worker) ProcessBatch(ctx context.Context, tasks []models.Task) {
	if len(tasks) == 0 {
		return
	}
	if len(tasks) == 1 {
		w.Process(ctx, tasks[0])
		return
	}

	defer func() {
		if r := recover(); r != nil {
			for _, task := range tasks {
				w.emit(ctx, task, "PANIC", fmt.Sprintf("worker batch panic: %v", r))
				w.failHard(ctx, task, fmt.Errorf("worker batch panic: %v", r))
			}
		}
	}()

	project, profile, err := w.loadContext(ctx, tasks[0])
	if err != nil || project == nil || profile == nil {
		if err == nil {
			err = fmt.Errorf("batch context missing for project=%q agent=%q", tasks[0].ProjectID, tasks[0].AgentID)
		}
		for _, task := range tasks {
			w.failHard(ctx, task, err)
		}
		return
	}

	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, w.store))

	runnable, stopHeartbeats := w.prepareBatchRunnable(ctx, tasks, *profile)
	defer stopHeartbeats()

	if len(runnable) < 2 {
		for _, task := range runnable {
			w.processRunningTask(ctx, task, *project, *profile)
		}
		return
	}

	if profile.AgenticMode {
		w.processBatchAgentic(ctx, runnable, *project, *profile)
		return
	}
	w.processBatchLegacy(ctx, runnable, *project, *profile)
}

func (w *Worker) prepareBatchRunnable(
	ctx context.Context,
	tasks []models.Task,
	profile models.AgentProfile,
) (runnable []models.Task, stopHeartbeats func()) {
	heartbeats := make([]func(), 0, len(tasks))
	for _, task := range tasks {
		if planning.IsPhasePlanningTask(task.Title) {
			w.Process(ctx, task)
			continue
		}
		running, runErr := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, os.Getpid())
		if runErr != nil {
			slog.Warn("batch mark task running failed", "task_id", task.ID, "error", runErr)
			w.requeue(ctx, task, fmt.Sprintf("mark running: %v", runErr))
			continue
		}
		task = *running
		if profile.RequireReview {
			if done, finErr := w.tryFinalizeApprovedReview(ctx, task); finErr != nil {
				w.failHard(ctx, task, finErr)
				continue
			} else if done {
				continue
			}
		}
		heartbeats = append(heartbeats, w.startHeartbeat(ctx, task.ID))
		runnable = append(runnable, task)
	}
	return runnable, func() {
		for _, stop := range heartbeats {
			stop()
		}
	}
}

func (w *Worker) processBatchAgentic(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	resp, err := w.runBatchTextGateway(ctx, tasks, project, profile)
	if err != nil {
		for _, task := range tasks {
			w.handleGatewayError(ctx, task, err)
		}
		return
	}
	bySlot := make(map[int]batchTextSlot, len(resp.Results))
	for _, r := range resp.Results {
		bySlot[r.Slot] = r
	}
	for i, task := range tasks {
		slot, ok := bySlot[i]
		if !ok || strings.TrimSpace(slot.Content) == "" {
			w.processRunningTask(ctx, task, project, profile)
			continue
		}
		p := profile
		w.commitTextWithProfile(ctx, task, slot.Content, &p)
	}
}

func (w *Worker) processBatchLegacy(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	resp, err := w.runBatchLegacyGateway(ctx, tasks, project, profile)
	if err != nil {
		for _, task := range tasks {
			w.handleGatewayError(ctx, task, err)
		}
		return
	}
	bySlot := make(map[int]batchLegacySlot, len(resp.Results))
	for _, r := range resp.Results {
		bySlot[r.Slot] = r
	}
	for i, task := range tasks {
		slot, ok := bySlot[i]
		if !ok {
			w.processRunningTask(ctx, task, project, profile)
			continue
		}
		if validateBatchLegacySlot(slot) != nil {
			w.processRunningTask(ctx, task, project, profile)
			continue
		}
		w.applyBatchLegacySlot(ctx, task, project, profile, slot)
	}
}

func (w *Worker) applyBatchLegacySlot(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	slot batchLegacySlot,
) {
	if slot.TooComplex {
		w.handleLegacyTaskBreakdown(ctx, task, slot.Subtasks)
		return
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.registerCancel(task.ID, cancel)
	defer w.deregisterCancel(task.ID)
	result, runErr := w.sandbox.Execute(execCtx, w.payload(task, project, slot.Command))
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, project, slot.Command, result)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, slot.Command, result)
		return
	}
	if profile.RequireReview && runErr == nil && result.Success {
		w.createReviewHandoff(ctx, task, result.Stdout)
		return
	}
	w.commit(ctx, task, result, runErr)
}
