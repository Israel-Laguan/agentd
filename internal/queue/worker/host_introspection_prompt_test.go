package worker

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

// Host facts must come from commands that need no files outside the workspace;
// the sandbox refuses /etc, /proc and /sys reads, so the prompt has to say so.
func TestWorkerSystemPromptDirectsHostIntrospectionToBinaries(t *testing.T) {
	t.Parallel()
	msgs := workerMessages(models.Task{Title: "t", Description: "d"}, models.AgentProfile{})
	system := msgs[0].Content
	for _, want := range []string{"hostnamectl", "lscpu", "free -h", "lsblk", "uname -a", "/etc", "only values printed by commands you ran"} {
		if !strings.Contains(system, want) {
			t.Fatalf("worker system prompt missing %q:\n%s", want, system)
		}
	}
}
