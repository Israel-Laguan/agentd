package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
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
	executor := agenttools.NewToolExecutor(&fakeSuccessExecutor{}, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "sess-wiring", "proj-wiring", "", time.Now(),
		call, nil, executor, nil, nil,
	)
	if suspended {
		t.Fatal("expected suspend=false")
	}
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("status = %s, want vetoed for credential in args", tr.Status)
	}
	if !strings.Contains(tr.Content, "credential pattern") {
		t.Fatalf("veto reason %q should mention credential pattern", tr.Content)
	}
}

func TestNewWorker_DisableCredentialDetection_SkipsHook(t *testing.T) {
	const envKey = "TEST_WIRING_DISABLE_DETECTION"
	t.Setenv(envKey, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij")

	w := NewWorker(nil, nil, &fakeSuccessExecutor{}, nil, nil, WorkerOptions{
		ToolCredentials:            map[string]string{"github": envKey},
		DisableCredentialDetection: true,
	})

	call := gateway.ToolCall{
		ID: "call-wiring-disable-1",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"git clone https://ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij@github.com/org/repo"}`,
		},
	}
	executor := agenttools.NewToolExecutor(&fakeSuccessExecutor{}, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(), "sess-wiring-disable", "proj-wiring-disable", "", time.Now(),
		call, nil, executor, nil, nil,
	)
	if suspended {
		t.Fatal("expected suspend=false")
	}
	if tr.Status != agenttools.ToolStatusSuccess {
		t.Fatalf("status = %s, want %s when credential detection disabled", tr.Status, agenttools.ToolStatusSuccess)
	}
}

func TestWorker_runSessionStart_FailsOnMissingCredential(t *testing.T) {
	envKey := "TEST_MISSING_" + strings.ReplaceAll(t.Name(), "/", "_")
	t.Setenv(envKey, "")

	w := NewWorker(nil, nil, &fakeSuccessExecutor{}, nil, nil, WorkerOptions{
		ToolCredentials: map[string]string{"github": envKey},
	})
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-session-start"}}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "proj-session-start"}}

	err := w.RunSessionStart(context.Background(), task, project)
	if err == nil {
		t.Fatal("RunSessionStart() = nil, want error for missing credential")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Fatalf("error %q does not mention tool name", err.Error())
	}
}

func TestWorker_RunSessionStart_SucceedsWhenCredentialsPresent(t *testing.T) {
	const envKey = "TEST_PRESENT_SESSION_START"
	t.Setenv(envKey, "present-value")

	w := NewWorker(nil, nil, &fakeSuccessExecutor{}, nil, nil, WorkerOptions{
		ToolCredentials: map[string]string{"github": envKey},
	})
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-session-start-ok"}}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "proj-session-start-ok"}}

	if err := w.RunSessionStart(context.Background(), task, project); err != nil {
		t.Fatalf("RunSessionStart() = %v, want nil", err)
	}
}
