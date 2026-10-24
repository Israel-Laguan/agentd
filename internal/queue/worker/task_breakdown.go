package worker

import (
	"context"
	"fmt"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

const breakdownDepthSanityCap = 32

// breakdownDepth returns how many parent tasks lie above taskID in the breakdown tree.
func (w *Worker) breakdownDepth(ctx context.Context, taskID string) (int, error) {
	depth := 0
	current := taskID
	for depth < breakdownDepthSanityCap {
		parents, err := w.store.ListParentTasks(ctx, current)
		if err != nil {
			return 0, err
		}
		if len(parents) == 0 {
			return depth, nil
		}
		depth++
		current = parents[0].ID
	}
	return depth, fmt.Errorf("breakdown depth exceeded sanity cap %d", breakdownDepthSanityCap)
}

// rejectBreakdown hands a split that breaks a cap to a human: a healing split
// gets a healing handoff, the legacy too_complex reply a legacy-mode handoff.
func (w *Worker) rejectBreakdown(ctx context.Context, task models.Task, cause string, healingCap bool) {
	if healingCap {
		w.createHealingHandoff(ctx, task, planning.HealingAction{
			Type:     planning.HealingActionHuman,
			StepName: planning.HealingStepHumanHandoff,
			Reason:   cause,
		}, cause)
		return
	}
	w.createLegacyModeHandoff(ctx, task, cause, nil)
}

// applyBreakdown turns subtasks into child tasks of task, within the split caps.
func (w *Worker) applyBreakdown(ctx context.Context, task models.Task, subtasks []planning.SplitSubtask, healingCap bool) {
	if len(subtasks) == 0 {
		w.handleAgentFailure(ctx, task, "worker reported task too complex without subtasks")
		return
	}
	if w.splitMaxSubtasks > 0 && len(subtasks) > w.splitMaxSubtasks {
		cause := fmt.Sprintf("Model returned %d subtasks but at most %d are allowed per split.",
			len(subtasks), w.splitMaxSubtasks)
		w.rejectBreakdown(ctx, task, cause, healingCap)
		return
	}
	if w.splitMaxDepth > 0 {
		depth, err := w.breakdownDepth(ctx, task.ID)
		if err != nil {
			cause := fmt.Sprintf("Breakdown sanity check failed: %v", err)
			w.rejectBreakdown(ctx, task, cause, healingCap)
			return
		}
		if depth >= w.splitMaxDepth {
			cause := fmt.Sprintf("Breakdown depth %d reached limit %d; further decomposition is not supported.",
				depth, w.splitMaxDepth)
			w.rejectBreakdown(ctx, task, cause, healingCap)
			return
		}
	}
	w.handleTaskBreakdown(ctx, task, subtasks)
}
