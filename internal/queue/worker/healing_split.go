package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/stage"
)

// handleHealingSplit breaks a task that keeps failing into subtasks, or hands
// it to a human when the split stage cannot.
func (w *Worker) handleHealingSplit(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	w.Emit(ctx, task, "HEALING_SPLIT", fmt.Sprintf("attempt=%d step=%s", task.RetryCount, planning.HealingStepSplitTask))
	result, err := w.splitTask(ctx, task, project, profile)
	if err != nil {
		w.createHealingHandoff(ctx, task, planning.HealingAction{
			Type:     planning.HealingActionHuman,
			StepName: planning.HealingStepHumanHandoff,
			Reason:   "failed to ask model for task breakdown: " + err.Error(),
		}, err.Error())
		return
	}
	if len(result.Subtasks) == 0 {
		w.createHealingHandoff(ctx, task, planning.HealingAction{
			Type:     planning.HealingActionHuman,
			StepName: planning.HealingStepHumanHandoff,
			Reason:   "model could not split task after repeated failures",
		}, "split stage returned no subtasks")
		return
	}
	w.applyBreakdown(ctx, task, result.Subtasks)
}

// splitTask is the split stage's one model call. Its context and contract come
// from planning; the worker adds memories and failure feedback.
func (w *Worker) splitTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (planning.SplitResult, error) {
	messages := planning.SplitMessages(task)
	messages = w.appendMemoryLessons(ctx, task.Title+" "+task.Description, task.ProjectID, project.UserID, messages)
	messages, _ = w.PrependReviewRejectionFeedback(ctx, task, messages)
	messages = w.PrependSandboxViolationFeedback(ctx, task, messages)
	profile = w.ApplyModelRouting(task, profile, messages, nil)
	res, err := w.taskCreationAgent().Call(gateway.WithTaskID(ctx, task.ID), stage.StageRequest{
		Stage:       stage.StageTaskCreation,
		Purpose:     gateway.PurposeCreateSplitTask,
		Context:     messages,
		Contract:    stage.NewContract[planning.SplitResult](),
		Temperature: profile.Temperature,
		Role:        gateway.RoleWorker,
		AgentID:     task.AgentID,
		Provider:    profile.Provider,
		Model:       profile.Model,
		MaxTokens:   profile.MaxTokens,
	})
	w.RecordTaskTokenUsage(ctx, task, res.TokenUsage, res.UsageDetails)
	if err != nil {
		return planning.SplitResult{}, err
	}
	var result planning.SplitResult
	if err := json.Unmarshal(res.Output, &result); err != nil {
		return planning.SplitResult{}, fmt.Errorf("%w: %v", models.ErrInvalidJSONResponse, err)
	}
	w.RecordProviderSuccess(ctx, task)
	return result, nil
}
