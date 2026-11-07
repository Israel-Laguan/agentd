package main

import (
	"context"
	"os"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/queue"
	"agentd/internal/sandbox"
)

// bashCallBackend answers the first agentic turn (a request that offers tools)
// with a bash tool call, and anything else with a final answer.
type bashCallBackend struct{ turns *int }

func (bashCallBackend) Name() spec.Provider { return "fake" }
func (bashCallBackend) MaxInputChars() int  { return 0 }
func (bashCallBackend) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func (b bashCallBackend) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if len(req.Tools) == 0 {
		return spec.AIResponse{Content: `{"needs_clarification": false}`, ProviderUsed: "fake", ModelUsed: "fake-model"}, nil
	}
	if *b.turns++; *b.turns > 1 {
		return spec.AIResponse{Content: "Done.", ProviderUsed: "fake", ModelUsed: "fake-model"}, nil
	}
	return spec.AIResponse{ProviderUsed: "fake", ModelUsed: "fake-model", ToolCalls: []spec.ToolCall{{
		ID: "call_bash", Type: "function",
		Function: spec.ToolCallFunction{Name: "bash", Arguments: `{"command":"touch notes.txt"}`},
	}}}, nil
}

type countingSandbox struct{ calls int }

func (s *countingSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	s.calls++
	return sandbox.Result{Success: true}, nil
}

type discardSink struct{}

func (discardSink) Emit(context.Context, models.Event) error { return nil }

// T-052: a profile stored with gated_tools ["bash"] stops an agentic task's
// bash call for a HUMAN approval. The gate was wired, but the store dropped
// the field, so it never fired (B-065).
func TestStoredGatedToolStopsForHumanApproval(t *testing.T) {
	ctx := context.Background()
	store := openUsageTestStore(t)
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "gated",
		Tasks: []models.DraftTask{{Title: "Write notes", Description: "Write a notes file in the workspace."}}})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{ID: tasks[0].AgentID, Name: "gated",
		Provider: "fake", Model: "fake-model", AgenticMode: true, GatedTools: []string{"bash"}}); err != nil {
		t.Fatalf("UpsertAgentProfile: %v", err)
	}
	project, err := store.GetProject(ctx, tasks[0].ProjectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if err := os.MkdirAll(project.WorkspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimNextReadyTasks = %v, %v", claimed, err)
	}

	var turns int
	sb := &countingSandbox{}
	w := queue.NewWorker(store, gateway.NewRouter(bashCallBackend{turns: &turns}), sb, queue.NewCircuitBreaker(),
		discardSink{}, queue.WorkerOptions{MaxToolIterations: 3})
	w.Process(ctx, claimed[0])

	got, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED: bash is gated on the stored profile", got.State)
	}
	children, err := store.ListChildTasks(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if len(children) != 1 || children[0].Title != models.HITLSubtaskTitleApproveTool+"bash" || children[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("children = %+v, want one HUMAN approval subtask for bash", children)
	}
	if sb.calls != 0 {
		t.Fatalf("sandbox ran %d times, want 0: the gated call waits for approval", sb.calls)
	}
}
