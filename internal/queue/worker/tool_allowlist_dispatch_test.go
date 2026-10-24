package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// dispatchWithProfile runs one tool call through the same task hook chain the
// agentic loop uses for a profile. It reports the result and how many times the
// sandbox ran; builtin write calls skip the sandbox, so tests check the
// workspace for those.
func dispatchWithProfile(t *testing.T, workspace string, profile models.AgentProfile, call gateway.ToolCall) (agenttools.ToolResult, int) {
	t.Helper()
	sb := &batchCountingSandbox{}
	w := NewWorker(&mockAgenticStore{}, nil, sb, nil, &mockEventSink{}, WorkerOptions{})
	taskHooks, _ := w.MountAgenticHooks(models.Project{}, profile)
	executor := agenttools.NewToolExecutor(sb, workspace, agenttools.BuildSandboxEnv(nil, nil), 0)

	tr, _ := w.DispatchToolWithHooks(context.Background(), "task-allowlist", "", "turn-1", time.Time{},
		call, nil, executor, taskHooks, nil, profile.Provider)
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return tr, sb.calls
}

const writeCallArgs = `{"path":"x.txt","content":"x"}`

// The model may name a tool it was never offered. With AllowedTools set, that
// call is vetoed before it runs, so trimming the offered list is not the only
// barrier.
func TestAllowedToolsVetoesUnlistedWriteAtDispatch(t *testing.T) {
	t.Parallel()
	profile := models.AgentProfile{AgenticMode: true, Provider: "openai", AllowedTools: []string{"bash", "read"}}
	call := gateway.ToolCall{ID: "call_write", Function: gateway.ToolCallFunction{Name: "write", Arguments: writeCallArgs}}
	workspace := t.TempDir()

	tr, _ := dispatchWithProfile(t, workspace, profile, call)
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("write status = %s, want vetoed: write is not in AllowedTools", tr.Status)
	}
	if _, err := os.Stat(filepath.Join(workspace, "x.txt")); err == nil {
		t.Fatal("vetoed write created x.txt; the veto must stop the call before it runs")
	}
}

// delegate starts a subagent whose tool set is every builtin tool, so an open
// delegate is a way round the allowlist. It is vetoed like any other name.
func TestAllowedToolsVetoesDelegateAtDispatch(t *testing.T) {
	t.Parallel()
	profile := models.AgentProfile{AgenticMode: true, Provider: "openai", AllowedTools: []string{"bash", "read"}}
	call := gateway.ToolCall{ID: "call_delegate", Function: gateway.ToolCallFunction{Name: agenttools.ToolNameDelegate, Arguments: `{"subagent":"any","task":"write the file"}`}}

	tr, _ := dispatchWithProfile(t, t.TempDir(), profile, call)
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("delegate status = %s, want vetoed: delegate is not in AllowedTools", tr.Status)
	}
}

func TestAllowedToolsVetoesDelegateParallelAtDispatch(t *testing.T) {
	t.Parallel()
	profile := models.AgentProfile{AgenticMode: true, Provider: "openai", AllowedTools: []string{"bash", "read"}}
	call := gateway.ToolCall{ID: "call_parallel", Function: gateway.ToolCallFunction{Name: agenttools.ToolNameDelegateParallel, Arguments: `{"tasks":[{"subagent":"any","task":"write"}]}`}}

	tr, _ := dispatchWithProfile(t, t.TempDir(), profile, call)
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("delegate_parallel status = %s, want vetoed", tr.Status)
	}
}

// A listed tool still runs; the veto only removes names outside the list.
func TestAllowedToolsRunsListedTool(t *testing.T) {
	t.Parallel()
	profile := models.AgentProfile{AgenticMode: true, Provider: "openai", AllowedTools: []string{"bash", "read"}}
	call := gateway.ToolCall{ID: "call_bash", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`}}

	tr, calls := dispatchWithProfile(t, t.TempDir(), profile, call)
	if tr.Status == agenttools.ToolStatusVetoed {
		t.Fatalf("bash was vetoed although AllowedTools lists it: %s", tr.Content)
	}
	if calls != 1 {
		t.Fatalf("sandbox ran %d times for an allowed bash call, want 1", calls)
	}
}

// Without an allowlist nothing is vetoed, so existing profiles keep every tool.
func TestProfileWithoutAllowlistVetoesNothing(t *testing.T) {
	t.Parallel()
	profile := models.AgentProfile{AgenticMode: true, Provider: "openai"}
	call := gateway.ToolCall{ID: "call_write", Function: gateway.ToolCallFunction{Name: "write", Arguments: writeCallArgs}}
	workspace := t.TempDir()

	tr, _ := dispatchWithProfile(t, workspace, profile, call)
	if tr.Status == agenttools.ToolStatusVetoed {
		t.Fatalf("write was vetoed on a profile with no allowlist: %s", tr.Content)
	}
	if _, err := os.Stat(filepath.Join(workspace, "x.txt")); err != nil {
		t.Fatalf("write did not run on a profile with no allowlist: %v", err)
	}
}

// The offered list honours the allowlist even when tool manifest filtering is
// off, which is its default.
func TestFilterAgenticToolsAppliesAllowlistWithoutManifest(t *testing.T) {
	t.Parallel()
	w := NewWorker(&mockAgenticStore{}, nil, &batchCountingSandbox{}, nil, &mockEventSink{}, WorkerOptions{})
	tools := []gateway.ToolDefinition{{Name: "bash"}, {Name: "read"}, {Name: "write"}, {Name: "delegate"}}
	index := map[string]string{}
	profile := models.AgentProfile{AllowedTools: []string{"bash", "read"}}

	got, _ := w.FilterAgenticTools(tools, index, models.Task{}, profile)
	names := toolNamesFromDefinitions(got)
	if len(names) != 2 || names[0] != "bash" || names[1] != "read" {
		t.Fatalf("offered tools = %v, want [bash read]", names)
	}
}
