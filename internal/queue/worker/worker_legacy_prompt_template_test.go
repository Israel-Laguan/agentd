package worker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
)

// TestLegacyPath_CodeGenProfileKeepsJSONContractWithPromptLibrary proves that
// loading the prompt template library (as the daemon does once B-060 wires
// PromptTemplatesPath) leaves the legacy JSON command contract intact for a
// code_gen profile: CODE_PROMPT_BUILDER asks for raw source code, which the
// workerResponse parser cannot read, so it must stay off the legacy path.
func TestLegacyPath_CodeGenProfileKeepsJSONContractWithPromptLibrary(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:               "agent-1",
		Provider:         "ollama", // Non-agentic provider
		Model:            "llama3",
		AgenticMode:      false,
		ToolManifestType: agenttools.TaskTypeCodeGen,
	}
	_, store, gw, sb := newRoutingTest(profile)
	store.task.Title = "Implement add"
	store.task.Description = "Add function in math.go\nSignature:\nfunc Add(a, b int) int"
	// A missing file is the daemon default: the embedded templates still load.
	path := filepath.Join(t.TempDir(), "prompt_templates.json")
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5, PromptTemplatesPath: path})
	if w.promptLibrary == nil {
		t.Fatal("prompt library not loaded; the test would prove nothing")
	}
	if !w.shouldUseCodePromptTemplate(store.task, profile) {
		t.Fatal("code_gen profile should select the code template on the agentic path")
	}

	w.Process(context.Background(), store.task)

	if len(gw.requests) != 1 {
		t.Fatalf("expected 1 gateway request for legacy path, got %d", len(gw.requests))
	}
	req := gw.requests[0]
	if !req.JSONMode {
		t.Error("expected JSONMode=true for legacy path")
	}
	if len(req.Messages) < 2 || !strings.HasPrefix(req.Messages[0].Content, legacyJSONCommandSystemSentinel) {
		t.Fatalf("legacy system prompt must ask for the JSON command object, got %+v", req.Messages)
	}
	if !strings.HasPrefix(req.Messages[1].Content, "You are executing Task: Implement add") {
		t.Errorf("legacy user prompt should be the default task message, got %q", req.Messages[1].Content)
	}
	for i, msg := range req.Messages {
		if strings.Contains(msg.Content, "raw source code") {
			t.Errorf("message[%d] carries CODE_PROMPT_BUILDER text: %q", i, msg.Content)
		}
	}
	if sb.execCount != 1 {
		t.Errorf("sandbox executions = %d, want 1 (parsed JSON command)", sb.execCount)
	}
	if store.task.State != models.TaskStateCompleted {
		t.Errorf("task state = %s, want %s", store.task.State, models.TaskStateCompleted)
	}
}
