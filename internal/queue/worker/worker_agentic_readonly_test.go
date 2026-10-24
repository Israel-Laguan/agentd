package worker

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// payloadCapturingSandbox records every payload and reports success.
type payloadCapturingSandbox struct {
	mu       sync.Mutex
	payloads []sandbox.Payload
}

func (s *payloadCapturingSandbox) Execute(_ context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payloads = append(s.payloads, payload)
	return sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}, nil
}

// researcherProfile is the researcher's read-only shape: bash and read only,
// with the workspace read-only.
func researcherProfile(profile models.AgentProfile) models.AgentProfile {
	profile.AllowedTools = []string{"bash", "read"}
	profile.WorkspaceReadonly = true
	return profile
}

// A read-only profile sends its bash commands to the sandbox with the workspace
// read-only.
func TestAgenticReadOnlyProfileSendsBashReadOnly(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "ls"), {Content: "Listed."},
	}}
	sb := &payloadCapturingSandbox{}
	w, _, task, project, profile := agenticFixture(t, gw, sb, retryDraft(), nil)

	w.processAgentic(context.Background(), task, project, researcherProfile(profile))

	if len(sb.payloads) != 1 || !sb.payloads[0].ReadOnlyWorkspace {
		t.Fatalf("sandbox payloads = %+v, want one bash payload with ReadOnlyWorkspace", sb.payloads)
	}
}

// The researcher task "fix the typo you find" must not change the workspace,
// whether the model reaches for write or for a delegate: both are vetoed, and
// the bash command runs read-only.
func TestResearcherCannotChangeWorkspaceThroughWriteOrDelegate(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{ToolCalls: []gateway.ToolCall{{ID: "w1", Type: "function", Function: gateway.ToolCallFunction{
			Name: "write", Arguments: `{"path":"notes.txt","content":"changed"}`}}}},
		{ToolCalls: []gateway.ToolCall{{ID: "d1", Type: "function", Function: gateway.ToolCallFunction{
			Name: "delegate", Arguments: `{"subagent":"any","task":"write notes.txt"}`}}}},
		bashCall("c1", "sed -i 's/teh/the/' typo.txt"),
		{Content: "Found the typo: teh should read the."},
	}}
	sb := &payloadCapturingSandbox{}
	prep := func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "typo.txt"), []byte("teh\n"), 0o644); err != nil {
			t.Fatalf("seed typo.txt: %v", err)
		}
	}
	w, _, task, project, profile := agenticFixture(t, gw, sb, retryDraft(), prep)

	w.processAgentic(context.Background(), task, project, researcherProfile(profile))

	workspace := project.WorkspacePath
	if _, err := os.Stat(filepath.Join(workspace, "notes.txt")); err == nil {
		t.Fatal("write created notes.txt; a read-only researcher must not write")
	}
	content, err := os.ReadFile(filepath.Join(workspace, "typo.txt"))
	if err != nil || string(content) != "teh\n" {
		t.Fatalf("typo.txt = %q, %v; want it unchanged", content, err)
	}
	if len(sb.payloads) != 1 || !sb.payloads[0].ReadOnlyWorkspace {
		t.Fatalf("sandbox payloads = %+v, want the one bash command to run read-only", sb.payloads)
	}
}
