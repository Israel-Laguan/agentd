package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// newRoutingTest creates a Worker with mock dependencies for routing tests.
func newRoutingTest(profile models.AgentProfile) (*Worker, *routingTestStore, *routingTestGateway, *routingTestSandbox) {
	store := &routingTestStore{
		task: models.Task{
			BaseEntity:  models.BaseEntity{ID: "task-routing"},
			ProjectID:   "project-1",
			AgentID:     "agent-1",
			Description: testutil.AgenticTestTaskDescription(),
			State:       models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "project-1"},
			WorkspacePath: "/tmp/test-workspace",
		},
		profile: profile,
	}
	gw := &routingTestGateway{}
	cfgs := routingConfigsForProfile(profile.Provider)
	if len(cfgs) == 0 && strings.TrimSpace(profile.Provider) != "" {
		cfgs = nil
	} else if len(cfgs) == 0 {
		cfgs = []spec.ProviderConfig{toolCapableConfig("synth-openai", "openai")}
	}
	if len(cfgs) > 0 {
		r, err := buildTestRouter(cfgs...)
		if err != nil {
			panic("buildTestRouter: " + err.Error())
		}
		gw.router = r
	}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	return w, store, gw, sb
}

// TestRoutingDecision_AgenticModeTrue_ProviderSupported verifies by calling Process
// that an agentic profile on a tool-capable provider runs the inner loop.
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderSupported(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"openai", "anthropic"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    provider,
				Model:       "gpt-4",
				AgenticMode: true,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if gw.requests[0].JSONMode {
				t.Error("expected JSONMode=false for agentic path")
			}
			if len(gw.requests[0].Tools) == 0 {
				t.Error("expected tools in agentic path request")
			}
		})
	}
}

// TestRoutingDecision_AgenticModeTrue_EmptyProviderCascade verifies that agentic mode
// with empty profile provider uses the agentic path (gateway cascade), not legacy fallback.
func TestRoutingDecision_AgenticModeTrue_EmptyProviderCascade(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "",
		Model:       "",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)
	w.Process(context.Background(), store.task)

	if len(gw.requests) == 0 {
		t.Fatal("expected at least 1 gateway request")
	}
	if gw.requests[0].JSONMode {
		t.Error("expected JSONMode=false for agentic path with empty provider (cascade)")
	}
	if len(gw.requests[0].Tools) == 0 {
		t.Error("expected tools in agentic path request with empty provider (cascade)")
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderNotSupported verifies that an agentic
// profile on a provider without tool support fails its task. The legacy command
// path used to run here and report success without any tool work (B-080).
func TestRoutingDecision_AgenticModeTrue_ProviderNotSupported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Ollama", "ollama"},
		{"Azure OpenAI", "azure-openai"},
		{"Vertex", "vertex"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    tc.provider,
				Model:       "claude-3",
				AgenticMode: true,
			}
			w, store, gw, sb := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			assertRejectedWithoutModelCall(t, store, gw, sb, tc.provider)
		})
	}
}

// assertRejectedWithoutModelCall checks that a task on a tool-less provider
// failed with the tool-support error, and that no model request or command ran.
func assertRejectedWithoutModelCall(t *testing.T, store *routingTestStore, gw *routingTestGateway, sb *routingTestSandbox, provider string) {
	t.Helper()
	if len(gw.requests) != 0 {
		t.Fatalf("provider %q: gateway received %d requests, want none", provider, len(gw.requests))
	}
	if sb.execCount != 0 {
		t.Fatalf("provider %q: %d sandbox runs, want none", provider, sb.execCount)
	}
	if store.task.State != models.TaskStateFailed {
		t.Fatalf("provider %q: task state = %s, want FAILED", provider, store.task.State)
	}
	if store.result == nil || !strings.Contains(store.result.Payload, "cannot call tools") {
		t.Fatalf("provider %q: result = %#v, want the tool-support error", provider, store.result)
	}
}

// TestRoutingDecision_AgenticUnsupportedProvider_ShortTask_FailsWithoutModelCall verifies
// that an unsupported provider fails before agentic setup, so elicitation never blocks
// the task and no legacy command runs.
func TestRoutingDecision_AgenticUnsupportedProvider_ShortTask_FailsWithoutModelCall(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "ollama",
		Model:       "llama3",
		AgenticMode: true,
	}
	w, store, gw, sb := newRoutingTest(profile)
	store.task.Description = "fix the bug"

	w.Process(context.Background(), store.task)

	if store.task.State == models.TaskStateBlocked {
		t.Fatal("expected a rejection, not BLOCKED from agentic elicitation")
	}
	assertRejectedWithoutModelCall(t, store, gw, sb, "ollama")
}
