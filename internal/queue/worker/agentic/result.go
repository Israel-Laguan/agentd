package agentic

import "agentd/internal/models"

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

// settle records the terminal result of this run. Every settling path ends the
// loop, so the last one is the only one.
func (e *Engine) settle(r Result) {
	e.result = r
}
