package worker

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestWorkerUserMessageStatesWorkingDirectory(t *testing.T) {
	t.Parallel()
	user := workerMessages(models.Task{Title: "t", Description: "d"}, models.AgentProfile{})[1].Content
	if !strings.Contains(user, "current directory") || !strings.Contains(user, "relative") {
		t.Fatalf("worker task message = %q, want working directory stated", user)
	}
}
