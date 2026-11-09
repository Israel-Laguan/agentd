package worker

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestContextBudgetKeepsTaskAndHeadTailOfOutput(t *testing.T) {
	messages := []spec.PromptMessage{
		{Role: "user", Section: spec.SectionTask, Content: "constraint: do not push"},
		{Role: "user", Section: spec.SectionComments, Content: "START " + strings.Repeat("x", 300) + " END"},
	}
	got, err := fitStageContext(messages, 100)
	if err != nil {
		t.Fatal(err)
	}
	text := renderContext(got)
	if len(text) > 100 || !strings.Contains(text, "constraint: do not push") || !strings.Contains(text, "START ") || !strings.Contains(text, " END") || !strings.Contains(text, "bytes omitted") {
		t.Fatalf("budgeted context = %q (%d bytes)", text, len(text))
	}
}

func TestContextBudgetDropsOlderCopiesBeforeCutting(t *testing.T) {
	messages := []spec.PromptMessage{
		{Role: "user", Name: "read:file.go", Content: strings.Repeat("a", 60)},
		{Role: "user", Name: "read:file.go", Content: strings.Repeat("b", 60)},
	}
	got, err := fitStageContext(messages, 80)
	if err != nil || len(got) != 1 || got[0].Content != messages[1].Content {
		t.Fatalf("context = %+v, error = %v", got, err)
	}
}

func TestContextBudgetRejectsOversizedProtectedInstructions(t *testing.T) {
	_, err := fitStageContext([]spec.PromptMessage{{Role: "user", Section: spec.SectionTask, Content: strings.Repeat("x", 101)}}, 100)
	if err == nil {
		t.Fatal("oversized task must not be silently truncated")
	}
}

func TestContextBudgetDoesNotDiscardHandoff(t *testing.T) {
	messages := []spec.PromptMessage{{Role: "user", Name: "attempt_handoff", Content: strings.Repeat("x", 101)}}
	if _, err := fitStageContext(messages, 100); err == nil {
		t.Fatal("oversized handoff must not be silently lost")
	}
}
