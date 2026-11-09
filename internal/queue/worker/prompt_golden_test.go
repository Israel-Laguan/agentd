package worker

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"agentd/internal/models"
)

func TestBuiltinPromptGolden(t *testing.T) {
	w := &Worker{}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "fixed"}, Title: "Fix bug", Description: "Fix the login bug"}
	messages, _ := w.AssembleAgenticSystemPrompt(context.Background(), task, models.Project{}, models.AgentProfile{})
	got, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("builtin prompt: %d bytes", len(messages[0].Content)+len(messages[1].Content))
	want, err := os.ReadFile("testdata/builtin_prompt.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(append(got, '\n')) != string(want) {
		t.Fatalf("builtin prompt changed\ngot: %s\nwant: %s", got, want)
	}
}
