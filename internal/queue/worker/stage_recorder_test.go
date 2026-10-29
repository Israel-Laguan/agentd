package worker

import (
	"context"
	"encoding/json"
	"sync"

	"agentd/internal/gateway"
	"agentd/internal/stage"
)

// stageRecorder is a stage.Agent that returns a canned output (or err) and
// records each request with the task ID it saw in ctx.
type stageRecorder struct {
	mu      sync.Mutex
	output  string
	err     error
	tokens  int
	outcome stage.Outcome
	reqs    []stage.StageRequest
	taskIDs []string
}

func (a *stageRecorder) Call(ctx context.Context, req stage.StageRequest) (stage.StageResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reqs = append(a.reqs, req)
	a.taskIDs = append(a.taskIDs, gateway.TaskIDFromContext(ctx))
	outcome := a.outcome
	if outcome == "" {
		outcome = stage.OutcomeCompleted
	}
	res := stage.StageResult{Outcome: outcome, Output: json.RawMessage(a.output), TokenUsage: a.tokens}
	return res, a.err
}
