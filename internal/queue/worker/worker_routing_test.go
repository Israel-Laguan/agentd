package worker

import (
	"context"
	"strings"
	"testing"

	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
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
		r, err := buildTestCapabilityRouter(cfgs...)
		if err != nil {
			panic("buildTestCapabilityRouter: " + err.Error())
		}
		gw.router = r
	}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	return w, store, gw, sb
}

// TestRoutingDecision_AgenticModeFalse_LegacyPath verifies by calling Process
// that when AgenticMode is false, the gateway receives JSONMode requests (legacy path).
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeFalse_LegacyPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"OpenAI", "openai"},
		{"Anthropic", "anthropic"},
		{"Ollama", "ollama"},
		{"Empty provider", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    tc.provider,
				Model:       "gpt-4",
				AgenticMode: false,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			// Legacy path: gateway request should have JSONMode=true and no tools
			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if !gw.requests[0].JSONMode {
				t.Error("expected JSONMode=true for legacy path")
			}
			if len(gw.requests[0].Tools) > 0 {
				t.Error("expected no tools in legacy path request")
			}
		})
	}
}

func TestRoutingDecision_AgenticModeFalse_MakesOnlyLegacySingleCall(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: false,
	}
	w, store, gw, sb := newRoutingTest(profile)

	w.Process(context.Background(), store.task)

	if len(gw.requests) != 1 {
		t.Fatalf("expected exactly 1 legacy gateway call and zero agentic follow-up calls, got %d", len(gw.requests))
	}
	if !gw.requests[0].JSONMode {
		t.Fatal("expected legacy request to use JSON mode")
	}
	if len(gw.requests[0].Tools) != 0 {
		t.Fatalf("expected legacy request to advertise zero tools, got %d", len(gw.requests[0].Tools))
	}
	if sb.execCount != 1 {
		t.Fatalf("expected legacy command to execute once, got %d", sb.execCount)
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderSupported verifies by calling Process
// that when AgenticMode is true and provider supports agentic mode, the agentic path is taken.
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

// TestAgenticFallbackPreservesRoutedProvider verifies that when processAgentic routes to a
// high-context tier then falls back to legacy, runLegacyTask does not re-route with legacy
// (shorter) messages and change provider.
func TestAgenticFallbackPreservesRoutedProvider(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, sb := newRoutingTest(profile)
	w.modelRouter = agentruntime.NewModelRouter(config.ModelRoutingConfig{
		Enabled:               true,
		ContextTokenThreshold: 150000,
		Cheap:                 config.ModelTierTarget{Provider: "anthropic", Model: "claude-haiku"},
		Mid:                   config.ModelTierTarget{Provider: "anthropic", Model: "claude-sonnet"},
		High:                  config.ModelTierTarget{Provider: "ollama", Model: "llama3"},
	})

	task := store.task
	task.Description = "summarize this file"

	// Agentic-sized context selects High (ollama, non-agentic).
	huge := strings.Repeat("x", 600001)
	agenticMessages := []gateway.PromptMessage{
		{Role: "system", Content: huge},
		{Role: "user", Content: task.Description},
	}
	tools := []gateway.ToolDefinition{{Name: "bash", Description: "run shell commands"}}
	routed := w.ApplyModelRouting(task, profile, agenticMessages, tools)
	if routed.Provider != "ollama" || routed.Model != "llama3" {
		t.Fatalf("agentic route = %s/%s, want ollama/llama3", routed.Provider, routed.Model)
	}

	legacyRouted := w.routeLegacyProfile(context.Background(), task, store.project, profile)
	if legacyRouted.Provider != "anthropic" {
		t.Fatalf("legacy-only route provider = %q, want anthropic (proves re-route would differ)", legacyRouted.Provider)
	}

	w.RunLegacyTask(context.Background(), task, store.project, routed, true)

	if len(gw.requests) != 1 {
		t.Fatalf("expected 1 gateway request, got %d", len(gw.requests))
	}
	if gw.requests[0].Provider != "ollama" || gw.requests[0].Model != "llama3" {
		t.Fatalf("legacy fallback request = %s/%s, want ollama/llama3", gw.requests[0].Provider, gw.requests[0].Model)
	}
	if sb.execCount != 1 {
		t.Fatalf("expected 1 sandbox run, got %d", sb.execCount)
	}
}

// TestRoutingDecision_ModelRoutingToUnsupportedProvider_Rejects verifies that when model
// routing selects a provider without tool support, the task is rejected, not run legacy.
func TestRoutingDecision_ModelRoutingToUnsupportedProvider_Rejects(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "anthropic",
		Model:       "claude-3",
		AgenticMode: true,
	}
	w, store, gw, sb := newRoutingTest(profile)
	w.modelRouter = agentruntime.NewModelRouter(config.ModelRoutingConfig{
		Enabled:               true,
		ContextTokenThreshold: 150000,
		Cheap:                 config.ModelTierTarget{Provider: "ollama", Model: "llama3"},
		Mid:                   config.ModelTierTarget{Provider: "anthropic", Model: "claude-sonnet"},
		High:                  config.ModelTierTarget{Provider: "anthropic", Model: "claude-opus"},
	})
	store.task.Description = testutil.AgenticTestTaskDescription()

	w.Process(context.Background(), store.task)

	assertRejectedWithoutModelCall(t, store, gw, sb, "ollama (routed)")
}

// TestProcessAgentic_ManifestFilterDoesNotShrinkRoutingEstimate verifies the full
// Process → processAgentic path: model routing token estimates use the pre-filter
// tool registry while gateway turn requests use manifest-filtered tools.
func TestProcessAgentic_ManifestFilterDoesNotShrinkRoutingEstimate(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)
	registry := capabilities.NewRegistry()
	registry.Register("routing_bulk", fakeCapabilityCallAdapter{
		name: "routing_bulk",
		tools: []gateway.ToolDefinition{{
			Name:        "routing_bulk",
			Description: strings.Repeat("x", 600001),
			Parameters:  &gateway.FunctionParameters{Type: "object"},
		}},
	})
	w.modelRouter = agentruntime.NewModelRouter(testModelRoutingConfig())
	w.toolManifest = agenttools.NewToolManifest(config.ToolManifestConfig{
		Enabled:       true,
		MinConfidence: 0.35,
	})
	w.capabilities = registry

	store.profile.ToolManifestType = agenttools.TaskTypeSummarize
	store.task.Description = testutil.AgenticTestTaskDescription()

	w.Process(context.Background(), store.task)

	var req gateway.AIRequest
	var found bool
	for _, r := range gw.requests {
		if !r.JSONMode {
			req = r
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected agentic turn request among %d gateway calls", len(gw.requests))
	}
	if req.Provider != "anthropic" || req.Model != "claude-opus" {
		t.Fatalf("routed request = %s/%s, want anthropic/claude-opus (full tools exceed threshold)", req.Provider, req.Model)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("expected manifest-filtered zero tools in turn request, got %d: %v", len(req.Tools), toolNamesFromDefinitions(req.Tools))
	}
}
