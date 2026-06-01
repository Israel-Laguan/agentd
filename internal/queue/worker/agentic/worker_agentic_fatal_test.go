package agentic

import (
	"context"
	"strings"
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

type fatalMockHost struct {
	*noopHost
}

func (h *fatalMockHost) DispatchToolWithHooks(
	ctx context.Context,
	sessionID, projectID, turnID string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	taskCaps *capabilities.Registry,
	providerName string,
) (agenttools.ToolResult, bool) {
	hookCtx := agenthooks.HookContext{
		ExecCtx:       ctx,
		SessionID:     sessionID,
		ProjectID:     projectID,
		TurnID:        turnID,
		TaskUpdatedAt: taskUpdatedAt,
		ToolName:      call.Function.Name,
		Args:          call.Function.Arguments,
		CallID:        call.ID,
	}
	verdict := taskHooks.RunPre(hookCtx)
	if verdict.Veto {
		if strings.Contains(verdict.Result, "FatalError") {
			return agenttools.FatalResult(call.ID, verdict.Result, 0), false
		}
		return agenttools.VetoedResult(call.ID, verdict.Result), false
	}
	return agenttools.SuccessResult(call.ID, "success", 0), false
}

func TestHandleAgenticToolCalls_FatalAborts(t *testing.T) {
	t.Parallel()

	store := testutil.NewFakeStore()
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "inject-fatal",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{
				Veto:         true,
				ShortCircuit: true,
				Result:       `{"FatalError":"sandbox crash"}`,
			}, nil
		},
	})

	host := &fatalMockHost{noopHost: &noopHost{}}
	e := &Engine{
		config: Config{Store: store},
		host:   host,
	}

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-fatal"},
		ProjectID:  "proj-fatal",
	}
	resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
		ID:       "call_fatal",
		Type:     "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
	}}}
	ex := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent", task.ID)

	var messages []gateway.PromptMessage
	budgetGuard := agentruntime.NewBudgetGuard(nil, task.ID)
	abort, result, report := e.handleAgenticToolCalls(
		context.Background(), task, "", resp, &messages, nil, ex, taskHooks, nil, cm, agenttools.NewToolFailureTracker(0), 0, budgetGuard,
	)
	if !abort || !report {
		t.Fatal("expected fatal tool result to report LoopToolFailure")
	}
	if result.Status != agentruntime.LoopToolFailure {
		t.Fatalf("result.Status = %s, want tool_failure", result.Status)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1 tool result appended before abort", len(messages))
	}
	if !strings.Contains(messages[0].Content, "[FATAL]") {
		t.Fatalf("tool message = %q, want [FATAL] prefix", messages[0].Content)
	}
}
