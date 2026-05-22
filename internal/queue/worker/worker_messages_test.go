package worker

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestLegacySeedMessages_CodeGenUsesJSONWorkerMessages(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{promptLibrary: lib}
	task := models.Task{
		Title:       "Implement add",
		Description: "Add function in math.go\nSignature:\nfunc Add(a, b int) int",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeCodeGen}

	got := w.legacySeedMessages(task, models.Project{}, profile)
	want := workerMessages(task, profile)
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, len(want) = %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Fatalf("message[%d]: got role=%q content=%q, want role=%q content=%q",
				i, got[i].Role, got[i].Content, want[i].Role, want[i].Content)
		}
	}
	if strings.Contains(got[0].Content, "raw source code") {
		t.Fatalf("legacy seed must not use CODE_PROMPT_BUILDER system text: %q", got[0].Content)
	}
	if !strings.Contains(got[1].Content, "You are executing Task:") {
		t.Fatalf("legacy seed should use default task user prompt: %q", got[1].Content)
	}
}
