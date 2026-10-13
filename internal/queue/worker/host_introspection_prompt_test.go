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
	for _, want := range []string{"hostnamectl", "lscpu", "free -h", "lsblk", "uname -a", "/etc", "only values printed by commands you ran", "never type the facts yourself"} {
		if !strings.Contains(system, want) {
			t.Fatalf("worker system prompt missing %q:\n%s", want, system)
		}
	}
}

func TestWorkerPayloadCarriesTaskIntentForEvidenceGuard(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	p := w.payload(models.Task{Title: "Compile System Report", Description: "write report.md"}, models.Project{WorkspacePath: "/w"}, "uname -a")
	if !strings.Contains(p.Intent, "Compile System Report") || !strings.Contains(p.Intent, "write report.md") {
		t.Fatalf("payload intent = %q, want task title and description", p.Intent)
	}
}
