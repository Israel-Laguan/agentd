package planning

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestSplitResultRejectsASubtaskWithoutATitle(t *testing.T) {
	t.Parallel()
	r := SplitResult{Subtasks: []SplitSubtask{
		{Title: "a", Description: "d"},
		{Title: "  ", Description: "d"},
	}}
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "subtasks[1].title") {
		t.Fatalf("Validate() = %v, want an error naming subtasks[1].title", err)
	}
}

func TestSplitResultRejectsASubtaskWithoutADescription(t *testing.T) {
	t.Parallel()
	r := SplitResult{Subtasks: []SplitSubtask{{Title: "a", Description: "  "}}}
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "subtasks[0].description") {
		t.Fatalf("Validate() = %v, want an error naming subtasks[0].description", err)
	}
}

// An empty list is a valid answer: the model says the task cannot be split,
// and the worker hands it to a human.
func TestSplitResultAcceptsNoSubtasks(t *testing.T) {
	t.Parallel()
	if err := (SplitResult{}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestSplitResultSchemaHintNamesItsFields(t *testing.T) {
	t.Parallel()
	hint := SplitResult{}.SchemaHint()
	for _, want := range []string{"subtasks", "title", "description"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("SchemaHint() = %q, missing %q", hint, want)
		}
	}
}

func TestSplitMessagesCarryTheTaskAndTheContract(t *testing.T) {
	t.Parallel()
	task := models.Task{Title: "Build the report", Description: "collect host facts"}
	msgs := SplitMessages(task)
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("messages = %+v, want system then user", msgs)
	}
	if !strings.Contains(msgs[0].Content, SplitResult{}.SchemaHint()) {
		t.Errorf("system prompt does not carry the contract:\n%s", msgs[0].Content)
	}
	for _, want := range []string{task.Title, task.Description} {
		if !strings.Contains(msgs[1].Content, want) {
			t.Errorf("user message missing %q:\n%s", want, msgs[1].Content)
		}
	}
}
