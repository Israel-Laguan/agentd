package agentic

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// payloadSandbox records the payload of each command it is asked to run.
type payloadSandbox struct{ payloads []sandbox.Payload }

func (s *payloadSandbox) Execute(_ context.Context, p sandbox.Payload) (sandbox.Result, error) {
	s.payloads = append(s.payloads, p)
	return sandbox.Result{Success: true, Stdout: "ok"}, nil
}

// LOG_CHUNK events from an agentic bash call were tagged task "tool" and an
// empty project, so the store rejected every one (events.project_id is NOT
// NULL) and the error was dropped: no live output reached the board or SSE.
// The executor built for a task must run its commands under that task.
func TestAgenticToolExecutorRunsCommandsUnderTheTask(t *testing.T) {
	t.Parallel()
	sb := &payloadSandbox{}
	e := &Engine{config: Config{Sandbox: sb}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}, ProjectID: "project-1"}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}

	ex := e.newAgenticTaskToolExecutor(project, task)
	ex.Execute(context.Background(), gateway.ToolCall{ID: "c1", Type: "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`}})

	if len(sb.payloads) != 1 {
		t.Fatalf("sandbox calls = %d, want 1", len(sb.payloads))
	}
	if got := sb.payloads[0]; got.TaskID != "task-1" || got.ProjectID != "project-1" {
		t.Fatalf("payload task=%q project=%q, want task-1 / project-1", got.TaskID, got.ProjectID)
	}
}
