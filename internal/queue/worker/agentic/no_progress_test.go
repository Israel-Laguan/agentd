package agentic

import (
	"context"
	"fmt"
	"testing"
	"time"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// countingHost answers every tool call with "out-<n>" where n advances only
// when vary is set, so a test can pick a same-result or a changing-result run.
type countingHost struct {
	*noopHost
	vary  bool
	calls int
}

func (h *countingHost) DispatchToolWithHooks(
	_ context.Context, _, _, _ string, _ time.Time, call gateway.ToolCall,
	_ map[string]string, _ *agenttools.ToolExecutor, _ *agenthooks.HookChain, _ *capabilities.Registry, _ string,
) (agenttools.ToolResult, bool) {
	h.calls++
	out := "bash: go: command not found"
	if h.vary {
		out = fmt.Sprintf("out-%d", h.calls)
	}
	return agenttools.SuccessResult(call.ID, out, 0), false
}

// dispatchSameCall sends one identical `go test` tool call per turn and
// returns the last turn's outcome.
func dispatchSameCall(t *testing.T, e *Engine, turns int) (bool, agentruntime.LoopResult, bool) {
	t.Helper()
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-loop", UpdatedAt: time.Now()}, ProjectID: "proj"}
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent", task.ID)
	var (
		abort, report bool
		result        agentruntime.LoopResult
		messages      []gateway.PromptMessage
	)
	for turn := range turns {
		resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
			ID: fmt.Sprintf("call-%d", turn), Type: "function",
			Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"go test ./..."}`},
		}}}
		abort, result, report = e.handleAgenticToolCalls(
			context.Background(), task, models.AgentProfile{}, "", resp, &messages, nil, nil, nil, nil, cm,
			agenttools.NewToolFailureTracker(3), turn, agentruntime.NewBudgetGuard(nil, task.ID),
		)
		if abort {
			return abort, result, report
		}
	}
	return abort, result, report
}

// B-081: with no Go toolchain in the jail, the worker ran the same `go test`
// and got the same "command not found" cycle after cycle, then tried apt-get,
// until the 900 s watcher limit. The same call with the same result three
// times is no progress, so the loop stops and names why.
func TestHandleAgenticToolCalls_StopsOnRepeatedCallWithSameResult(t *testing.T) {
	t.Parallel()
	e := &Engine{config: Config{Store: testutil.NewFakeStore()}, host: &countingHost{noopHost: &noopHost{}}}

	abort, result, report := dispatchSameCall(t, e, 3)
	if !abort || !report {
		t.Fatalf("abort = %v report = %v after 3 identical calls with identical results, want a reported stop", abort, report)
	}
	if result.Status != agentruntime.LoopToolFailure || result.Meta.ErrorCode != agenttools.ToolErrorCodeNoProgress {
		t.Fatalf("result = %v code %q, want tool_failure with %q", result.Status, result.Meta.ErrorCode, agenttools.ToolErrorCodeNoProgress)
	}
	if result.Meta.ToolName != "bash" {
		t.Errorf("tool = %q, want bash", result.Meta.ToolName)
	}
}

// A repeated call whose result changes is progress (e.g. `go test` after an
// edit), and two identical results are not yet a loop.
func TestHandleAgenticToolCalls_RepeatedCallWithNewResultsContinues(t *testing.T) {
	t.Parallel()
	changing := &Engine{config: Config{Store: testutil.NewFakeStore()}, host: &countingHost{noopHost: &noopHost{}, vary: true}}
	if abort, _, _ := dispatchSameCall(t, changing, 5); abort {
		t.Fatal("stopped on a repeated call whose result changed each time")
	}
	twice := &Engine{config: Config{Store: testutil.NewFakeStore()}, host: &countingHost{noopHost: &noopHost{}}}
	if abort, _, _ := dispatchSameCall(t, twice, 2); abort {
		t.Fatal("stopped after only two identical results")
	}
}
