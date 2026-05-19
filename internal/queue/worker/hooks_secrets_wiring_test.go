package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
)

func TestNewWorker_CredentialDetectionBlocksArgs(t *testing.T) {
	const envKey = "TEST_WIRING_GH_TOKEN"
	t.Setenv(envKey, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij")

	w := NewWorker(nil, nil, &fakeSuccessExecutor{}, nil, nil, WorkerOptions{
		ToolCredentials: map[string]string{"github": envKey},
	})

	call := gateway.ToolCall{
		ID: "call-wiring-1",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"git clone https://ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij@github.com/org/repo"}`,
		},
	}
	executor := NewToolExecutor(&fakeSuccessExecutor{}, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "sess-wiring", "proj-wiring", time.Now(),
		call, nil, executor, nil, nil,
	)
	if suspended {
		t.Fatal("expected suspend=false")
	}
	if tr.Status != ToolStatusVetoed {
		t.Fatalf("status = %s, want vetoed for credential in args", tr.Status)
	}
	if !strings.Contains(tr.Content, "credential pattern") {
		t.Fatalf("veto reason %q should mention credential pattern", tr.Content)
	}
}
