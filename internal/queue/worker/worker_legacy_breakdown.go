package worker

import (
	"context"
	"fmt"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

const legacyPreflightUserNote = `LEGACY MODE CONSTRAINT: respond with exactly one JSON object containing a single shell command.
Use redirects and pipes to write files (e.g. find, du, tee). Do not embed large file contents in the command string.`

const legacyBreakdownDepthSanityCap = 32

// legacyBreakdownDepth returns how many parent tasks lie above taskID in the breakdown tree.
func (w *Worker) legacyBreakdownDepth(ctx context.Context, taskID string) (int, error) {
	depth := 0
	current := taskID
	for depth < legacyBreakdownDepthSanityCap {
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
	return depth, nil
}

func (w *Worker) legacyDispatchRejectReason(task models.Task, profile models.AgentProfile) string {
	if w.providerSupportsAgentic(profile) {
		return ""
	}
	score := (ComplexityScorer{}).ScoreTask(task)
	if w.legacyRejectScore > 0 && score >= w.legacyRejectScore {
		return fmt.Sprintf(
			"Task complexity score %d exceeds legacy reject threshold %d; agentic mode is required.",
			score, w.legacyRejectScore,
		)
	}
	if w.legacyMaxDescriptionLen > 0 && len(task.Description) > w.legacyMaxDescriptionLen {
		return fmt.Sprintf(
			"Task description length %d exceeds legacy limit %d; agentic mode is required.",
			len(task.Description), w.legacyMaxDescriptionLen,
		)
	}
	return ""
}

func (w *Worker) legacySeedMessages(task models.Task, _ models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	messages := workerMessages(task, profile)
	if w.legacyPreflightScore > 0 && !w.providerSupportsAgentic(profile) {
		if (ComplexityScorer{}).ScoreTask(task) >= w.legacyPreflightScore {
			messages = append(messages, gateway.PromptMessage{
				Role:    "user",
				Content: legacyPreflightUserNote,
			})
		}
	}
	return messages
}

func (w *Worker) handleLegacyTaskBreakdown(ctx context.Context, task models.Task, subtasks []workerSubtask) {
	if len(subtasks) == 0 {
		w.handleAgentFailure(ctx, task, "worker reported task too complex without subtasks")
		return
	}
	if w.legacyMaxSubtasksPerBreakdown > 0 && len(subtasks) > w.legacyMaxSubtasksPerBreakdown {
		w.createLegacyModeHandoff(ctx, task,
			fmt.Sprintf("Model returned %d subtasks but legacy mode allows at most %d per breakdown.",
				len(subtasks), w.legacyMaxSubtasksPerBreakdown),
			nil)
		return
	}
	if w.legacyMaxBreakdownDepth > 0 {
		depth, err := w.legacyBreakdownDepth(ctx, task.ID)
		if err != nil {
			w.emit(ctx, task, "ERROR", err.Error())
			return
		}
		if depth >= w.legacyMaxBreakdownDepth {
			w.createLegacyModeHandoff(ctx, task,
				fmt.Sprintf("Legacy breakdown depth %d reached limit %d; further decomposition is not supported.",
					depth, w.legacyMaxBreakdownDepth),
				nil)
			return
		}
	}
	w.handleTaskBreakdown(ctx, task, subtasks)
}
