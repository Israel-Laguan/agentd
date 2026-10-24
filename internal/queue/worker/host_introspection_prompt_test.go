package worker

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestWorkerPayloadCarriesTaskIntentForEvidenceGuard(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	p := w.payload(models.Task{Title: "Compile System Report", Description: "write report.md"}, models.Project{WorkspacePath: "/w"}, "uname -a")
	if !strings.Contains(p.Intent, "Compile System Report") || !strings.Contains(p.Intent, "write report.md") {
		t.Fatalf("payload intent = %q, want task title and description", p.Intent)
	}
}

func TestWorkerUserMessageStatesWorkingDirectory(t *testing.T) {
	t.Parallel()
	user := workerMessages(models.Task{Title: "t", Description: "d"}, models.AgentProfile{})[1].Content
	if !strings.Contains(user, "current directory") || !strings.Contains(user, "relative") {
		t.Fatalf("worker task message = %q, want working directory stated", user)
	}
}
