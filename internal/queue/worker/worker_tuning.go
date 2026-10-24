package worker

import (
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

func tuningAttempt(task models.Task, sessionRecoveryGen int) int {
	if sessionRecoveryGen > task.RetryCount {
		return sessionRecoveryGen
	}
	return task.RetryCount
}

func (w *Worker) ApplyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest {
	attempt := tuningAttempt(task, sessionRecoveryGen)
	if w.tuner == nil || attempt <= 0 {
		return req
	}
	action := w.tuner.ForAttempt(attempt, profile)
	if action.Type != planning.HealingActionTune {
		return req
	}
	req = w.tuner.Apply(req, action)
	if action.Overrides.Compress {
		req.Messages = append(req.Messages, gateway.PromptMessage{
			Role:    "user",
			Content: "Previous attempts failed. Minimize assumptions, reduce variables, and return the smallest safe next command.",
		})
	}
	return req
}
