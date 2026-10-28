package planning

import (
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// The split stage breaks a task that keeps failing into smaller subtasks. This
// file holds its context and its contract; the worker makes the one stage call
// (stage.StageTaskCreation) and applies the result.

const splitInstruction = "This task has failed several times as one unit. Break it into smaller subtasks " +
	"that can each be run and checked on their own, listed in the order they should run. " +
	"If it cannot be split usefully, return an empty subtasks list.\n"

// SplitSubtask is one piece of a task the split stage proposes.
type SplitSubtask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// SplitResult is the split stage's contract. An empty Subtasks list is a valid
// answer: the task cannot be split, and the worker hands it to a human.
type SplitResult struct {
	Subtasks []SplitSubtask `json:"subtasks"`
}

// Validate rejects a subtask with no title or description, so the correction
// loop asks again.
func (r SplitResult) Validate() error {
	for i, s := range r.Subtasks {
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("subtasks[%d].title is required", i)
		}
		if strings.TrimSpace(s.Description) == "" {
			return fmt.Errorf("subtasks[%d].description is required", i)
		}
	}
	return nil
}

// SchemaHint describes the JSON shape; it goes in the prompt and in corrective retries.
func (SplitResult) SchemaHint() string {
	return `Respond with a single JSON object and no prose or markdown fences: ` +
		`{"subtasks":[{"title":"short imperative title","description":"what to do and how to tell it is done"}]}`
}

// SplitMessages is the stage's context: the instruction with the contract, and
// the task to split. Callers add memories and feedback around it.
func SplitMessages(task models.Task) []gateway.PromptMessage {
	return []gateway.PromptMessage{
		{Role: "system", Content: splitInstruction + SplitResult{}.SchemaHint()},
		{Role: "user", Content: fmt.Sprintf("Task: %s\nDescription: %s", task.Title, task.Description)},
	}
}
