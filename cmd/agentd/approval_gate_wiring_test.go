package main

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"agentd/internal/agent/acp"
	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/queue"

	acpsdk "github.com/coder/acp-go-sdk"
)

// TestMain lets the command test binary also serve as a fake ACP agent.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == fakeagent.Arg {
		if err := fakeagent.Serve(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// T-052: the stored gated_tools value must suspend a real ACP permission call.
func TestStoredGatedToolStopsForHumanApproval(t *testing.T) {
	ctx := context.Background()
	store := openUsageTestStore(t)
	step := fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindExecute, RawInput: map[string]any{"command": "touch notes.txt"}}}
	description := strings.Repeat("Write notes for the requested task. ", 12) + "\n" + fakeagent.Script(step)
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "gated",
		Tasks: []models.DraftTask{{Title: "Write notes", Description: description, RequiresWorkspaceChange: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{ID: tasks[0].AgentID, Name: "gated", GatedTools: []string{"bash"}}); err != nil {
		t.Fatal(err)
	}
	project, err := store.GetProject(ctx, tasks[0].ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.WorkspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimNextReadyTasks = %v, %v", claimed, err)
	}

	acp.Presets["approval-test"] = acp.Preset{Command: os.Args[0], Allow: []string{"model.test:443"}, Home: func(acp.Launch) ([]string, error) {
		return []string{os.Args[0], fakeagent.Arg}, nil
	}}
	t.Cleanup(func() { delete(acp.Presets, "approval-test") })
	sessions := &agentmcp.SessionMux{}
	server := httptest.NewServer(sessions)
	t.Cleanup(server.Close)
	runtime := &queue.ACPRuntime{Sessions: sessions, MCPURL: server.URL, Start: func([]string, string) (queue.ACPRun, error) {
		return queue.ACPRun{Home: t.TempDir(), Command: func(ctx context.Context, _ string, argv []string, _ bool) (*exec.Cmd, error) {
			return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
		}, Close: func() {}}, nil
	}}
	routes := route.Table{Agents: map[string]route.Agent{"coder": {Kind: route.KindACP, Preset: "approval-test", Level: spec.LevelL3}},
		Stages: map[spec.Purpose]route.StageRoute{spec.PurposeExecTurn: {Default: "coder"}}}
	w := queue.NewWorker(store, nil, nil, queue.NewCircuitBreaker(), nil, queue.WorkerOptions{Routes: routes, ACP: runtime})
	w.Process(ctx, claimed[0])

	got, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED: bash is gated on the stored profile", got.State)
	}
	children, err := store.ListChildTasks(ctx, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Title != models.HITLSubtaskTitleApproveTool+"bash" || children[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("children = %+v, want one HUMAN approval subtask for bash", children)
	}
}
