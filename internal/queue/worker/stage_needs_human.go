package worker

import (
	"context"
	"fmt"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// HumanAsker hands a question to a person and blocks the task on the answer.
// Worker.AskHuman has this shape.
type HumanAsker func(ctx context.Context, task models.Task, question string, options []string) error

// handOffNeedsHuman is the one place a NeedsHuman stage result becomes a question
// to a person. The error wraps stage.ErrNeedsHuman in every case, so the caller
// stops its work on the task even when the question could not be recorded.
func handOffNeedsHuman(ctx context.Context, ask HumanAsker, task models.Task, purpose spec.Purpose) error {
	if ask == nil {
		return fmt.Errorf("%w: %s has no human handoff", stage.ErrNeedsHuman, purpose)
	}
	question := fmt.Sprintf("The %s step cannot finish without a decision. What should it do?", purpose)
	if err := ask(ctx, task, question, nil); err != nil {
		return fmt.Errorf("%w: hand off %s: %w", stage.ErrNeedsHuman, purpose, err)
	}
	return fmt.Errorf("%w: %s", stage.ErrNeedsHuman, purpose)
}
