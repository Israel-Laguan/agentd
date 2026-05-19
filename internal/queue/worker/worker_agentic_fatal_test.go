package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestHandleAgenticToolCalls_FatalAborts(t *testing.T) {
	t.Parallel()

	store := &mockAgenticStore{}
	taskHooks := NewHookChain()
	taskHooks.RegisterPre(PreHook{
		Name:   "inject-fatal",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{
				Veto:         true,
				ShortCircuit: true,
				Result:       `{"FatalError":"sandbox crash"}`,
			}, nil
		},
	})

	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{})
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-fatal"},
		ProjectID:  "proj-fatal",
	}
	resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
		ID:       "call_fatal",
		Type:     "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
	}}}
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", task.ID)

	var messages []gateway.PromptMessage
	suspended := w.handleAgenticToolCalls(
		context.Background(), task, "", resp, &messages, nil, ex, taskHooks, nil, cm,
	)
	if !suspended {
		t.Fatal("expected fatal tool result to suspend agentic loop")
	}
	if store.task.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1 after handleAgentFailure", store.task.RetryCount)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1 tool result appended before abort", len(messages))
	}
	if !strings.Contains(messages[0].Content, "[FATAL]") {
		t.Fatalf("tool message = %q, want [FATAL] prefix", messages[0].Content)
	}
}
