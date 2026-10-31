package agentic

import (
	"context"
	"fmt"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// ResultKind is how a task attempt ended when the engine decided it.
type ResultKind int

const (
	// ResultNone means no terminal result: the loop ended with a LoopResult, or
	// the run stopped at a human (elicitation) and the board is already set.
	ResultNone ResultKind = iota
	ResultCompleted
	ResultFailed
	ResultGatewayError
	ResultPromptHandoff
)

// Result is a terminal outcome the engine settled on. The worker applies it to
// the board, so the engine never writes it ("only the core writes the board",
// T-041).
type Result struct {
	Kind ResultKind
	// Task is the task as the engine held it when it settled. The board write
	// checks its UpdatedAt against the stored row, so the worker must get this
	// value, not a fresh read.
	Task    models.Task
	Err     error
	Text    string
	Payload string
	// Profile is the profile the final text was produced under (review routing).
	Profile models.AgentProfile
}

// CheckChatTools reports the terminal result for a provider that cannot call
// tools, and whether that refusal applies.
func CheckChatTools(gw gateway.AIGateway, task models.Task, provider string) (Result, bool) {
	if gateway.ProviderSupportsChatTools(gw, provider) {
		return Result{}, false
	}
	err := fmt.Errorf("%w (%q)", models.ErrAgentProfileNoToolSupport, provider)
	return Result{Kind: ResultFailed, Task: task, Err: err}, true
}

// criteriaWriter adapts Config.CriteriaMet to the goal tracker's updater.
type criteriaWriter func(ctx context.Context, taskID string, met []string) error

// UpdateCriteriaMet implements agentcontext.CriteriaUpdater. A nil callback
// drops the progress.
func (f criteriaWriter) UpdateCriteriaMet(ctx context.Context, taskID string, met []string) error {
	if f == nil {
		return nil
	}
	return f(ctx, taskID, met)
}
