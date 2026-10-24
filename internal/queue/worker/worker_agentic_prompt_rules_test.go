package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
)

// The agentic system prompt carries the host-fact and path rules the legacy
// JSON prompt has: the sandbox refuses /etc, /proc and /sys, and a report may
// state only values a command printed.
func TestAgenticSystemPromptDirectsHostFactsToCommands(t *testing.T) {
	t.Parallel()
	system := agenticSystemPromptFor(t)
	for _, want := range []string{"hostnamectl", "lscpu", "free -h", "lsblk", "uname -a", "/etc", "/proc", "/sys", "only values printed by commands you ran", "never type the facts yourself"} {
		if !strings.Contains(system, want) {
			t.Fatalf("agentic system prompt missing %q:\n%s", want, system)
		}
	}
}

func TestAgenticSystemPromptStatesWorkspaceIsCurrentDirectory(t *testing.T) {
	t.Parallel()
	system := agenticSystemPromptFor(t)
	for _, want := range []string{"current directory", "relative paths", "never /workspace"} {
		if !strings.Contains(system, want) {
			t.Fatalf("agentic system prompt missing %q:\n%s", want, system)
		}
	}
}

// agenticSystemPromptFor returns the system message an agentic attempt starts
// with, for a plain task.
func agenticSystemPromptFor(t *testing.T) string {
	t.Helper()
	w := NewWorker(&mockAgenticStore{}, nil, &batchCountingSandbox{}, nil, &mockEventSink{}, WorkerOptions{})
	msgs := w.AssembleAgenticSystemPrompt(context.Background(), models.Task{Title: "t", Description: "d"}, models.Project{}, models.AgentProfile{})
	if len(msgs) == 0 {
		t.Fatal("AssembleAgenticSystemPrompt returned no messages")
	}
	return msgs[0].Content
}
