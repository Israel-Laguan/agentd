package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// HumanAsker hands a question to a person and blocks the task on the answer.
// Worker.AskHuman has this shape.
type HumanAsker func(ctx context.Context, task models.Task, question string, options []string) error

// handOffNeedsHuman is the one place a NeedsHuman stage result becomes a question
// to a person. A successful or absent handoff wraps stage.ErrNeedsHuman so the
// caller stops its work on the task; a failed handoff returns its own error.
func handOffNeedsHuman(ctx context.Context, ask HumanAsker, task models.Task, purpose spec.Purpose, output json.RawMessage) error {
	if ask == nil {
		return fmt.Errorf("%w: %s has no human handoff", stage.ErrNeedsHuman, purpose)
	}
	question, options := needsHumanQuestion(purpose, output)
	if err := ask(ctx, task, question, options); err != nil {
		// The handoff failed before the blocking subtask was created, so the
		// task is not actually blocked on a human answer. Keep this distinct
		// from stage.ErrNeedsHuman so callers retry or fail it instead of
		// waiting for an answer that was never recorded.
		return fmt.Errorf("%w: hand off %s: %w", stage.ErrHumanHandoffFailed, purpose, err)
	}
	return fmt.Errorf("%w: %s", stage.ErrNeedsHuman, purpose)
}

// needsHumanQuestion reads the question and options a marked reply carries
// ({"needs_human": true, "question": "...", "options": [...]}). A reply without
// a question gets a generic one that names the purpose.
func needsHumanQuestion(purpose spec.Purpose, output json.RawMessage) (string, []string) {
	var marked struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if json.Unmarshal(output, &marked) == nil && strings.TrimSpace(marked.Question) != "" {
		return strings.TrimSpace(marked.Question), marked.Options
	}
	return fmt.Sprintf("The %s step cannot finish without a decision. What should it do?", purpose), nil
}
