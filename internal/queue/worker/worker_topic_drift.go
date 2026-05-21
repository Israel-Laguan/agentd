package worker

import (
	"context"
	"errors"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

var errTopicDriftReset = errors.New("topic drift: session reset")

// guardTopicDrift runs before context compression when new human input arrives.
// Returns true when the session was reset and the turn loop should restart at turn 0.
func (w *Worker) guardTopicDrift(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	turnIndex int,
	messages *[]gateway.PromptMessage,
	sessionMgr *SessionManager,
) (bool, error) {
	if turnIndex == 0 || w.topicGuard == nil || sessionMgr == nil {
		return false, nil
	}
	newInput, ok := sessionMgr.PollNewHumanInput(ctx, w.store, task.ID)
	if !ok || newInput == "" {
		return false, nil
	}
	drift, err := w.topicGuard.DetectDrift(ctx, sessionMgr.TopicAnchor(), newInput, profile)
	if err != nil {
		return false, err
	}
	if !drift {
		sessionMgr.AdvanceTopic(newInput)
		return false, nil
	}
	if _, err := sessionMgr.ArchiveAndReset(ctx, w, task, project, profile, messages, newInput); err != nil {
		return false, err
	}
	return true, errTopicDriftReset
}

// newAgenticContextManagerOnly returns a fresh ContextManager without goal tracker side effects.
func (w *Worker) newAgenticContextManagerOnly(task models.Task) *ContextManager {
	cm, _ := w.newAgenticContextManager(task)
	return cm
}
