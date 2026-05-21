package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func enabledCapabilityRouter() *CapabilityRouter {
	return NewCapabilityRouter(config.CapabilityRoutingConfig{
		Enabled:       true,
		MinConfidence: 0.35,
		Mappings: map[string]string{
			IntentGenerateImage:  "stability_api",
			IntentRealTimeSearch: "serper_api",
		},
		Tools: map[string]string{
			IntentGenerateImage: "generate",
		},
	})
}

func TestIntentClassifier_BrowseURL_HTTPS(t *testing.T) {
	t.Parallel()
	c := NewIntentClassifier(0.35)
	task := models.Task{
		Title:       "Browse https://example.com",
		Description: "fetch page content from this url",
	}
	got := c.Classify(task)
	if got.Intent != IntentBrowseURL {
		t.Fatalf("Intent = %q, want %q", got.Intent, IntentBrowseURL)
	}
	if got.Confidence < 0.35 {
		t.Fatalf("Confidence = %v, want >= 0.35", got.Confidence)
	}
}

func TestIntentClassifier_TieReturnsNoIntent(t *testing.T) {
	t.Parallel()
	c := NewIntentClassifier(0)
	task := models.Task{
		Title:       "latest url",
		Description: "",
	}
	got := c.Classify(task)
	if got.Intent != "" {
		t.Fatalf("Intent = %q, want empty on tied scores", got.Intent)
	}
	if got.Scores[IntentRealTimeSearch] != 1 || got.Scores[IntentBrowseURL] != 1 {
		t.Fatalf("Scores = %+v, want 1 hit each on real_time_search and browse_url", got.Scores)
	}
}

func TestIntentClassifier_GenerateImage(t *testing.T) {
	t.Parallel()
	c := NewIntentClassifier(0.35)
	task := models.Task{
		Title:       "Generate an image",
		Description: "Draw a logo illustration for the product",
	}
	got := c.Classify(task)
	if got.Intent != IntentGenerateImage {
		t.Fatalf("Intent = %q, want %q", got.Intent, IntentGenerateImage)
	}
	if got.Confidence < 0.35 {
		t.Fatalf("Confidence = %v, want >= 0.35", got.Confidence)
	}
}

func TestCapabilityRouter_NoMatch(t *testing.T) {
	t.Parallel()
	r := enabledCapabilityRouter()
	task := models.Task{
		Title:       "Implement feature",
		Description: "Refactor the authentication module and add unit tests",
	}
	_, ok := r.Route(task, models.AgentProfile{})
	if ok {
		t.Fatal("Route() = true, want false for generic coding task")
	}
}

func TestCapabilityRouter_ProfileForcedIntent(t *testing.T) {
	t.Parallel()
	r := enabledCapabilityRouter()
	task := models.Task{Title: "Implement feature", Description: "Refactor code"}
	profile := models.AgentProfile{CapabilityRouteIntent: IntentGenerateImage}
	decision, ok := r.Route(task, profile)
	if !ok {
		t.Fatal("Route() = false, want true for forced intent")
	}
	if decision.Intent != IntentGenerateImage || decision.Adapter != "stability_api" {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Tool != "generate" {
		t.Fatalf("Tool = %q, want generate", decision.Tool)
	}
}

func TestCapabilityRouter_AdaptTask(t *testing.T) {
	t.Parallel()
	r := enabledCapabilityRouter()
	task := models.Task{Title: "Title", Description: "Body"}
	args := r.AdaptTask(task, CapabilityRouteDecision{})
	if args["prompt"] != "Title\nBody" {
		t.Fatalf("prompt = %v, want Title\\nBody", args["prompt"])
	}
}

func TestCapabilityRouter_DisabledReturnsNil(t *testing.T) {
	t.Parallel()
	if NewCapabilityRouter(config.CapabilityRoutingConfig{Enabled: false}) != nil {
		t.Fatal("NewCapabilityRouter(disabled) should return nil")
	}
}

func TestTryExternalCapabilityRoute_CommitsResultAndMessages(t *testing.T) {
	t.Parallel()

	committed := ""
	store := &mockCommitStore{text: &committed}
	registry := capabilities.NewRegistry()
	registry.Register("stability_api", fakeCapabilityCallAdapter{
		name: "stability_api",
		tools: []gateway.ToolDefinition{{
			Name:        "generate",
			Description: "generate image",
		}},
	})

	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{Capabilities: registry})
	w.capabilityRouter = enabledCapabilityRouter()

	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-cap"},
		Title:       "Generate an image",
		Description: "Draw a picture of a cat",
	}
	profile := models.AgentProfile{ID: "agent-1"}

	result, ok, err := w.tryExternalCapabilityRoute(context.Background(), task, models.Project{}, profile, &messages)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("tryExternalCapabilityRoute() = false, want true")
	}
	if result.Status != LoopSuccessfulCompletion {
		t.Fatalf("status = %v, want successful_completion", result.Status)
	}
	if !strings.Contains(committed, "generate") {
		t.Fatalf("committed = %q, want adapter output", committed)
	}
	if len(messages) != 3 || messages[2].Role != "assistant" {
		t.Fatalf("messages = %+v, want assistant commit appended", messages)
	}
}

func TestTryExternalCapabilityRoute_CallToolError_Terminal(t *testing.T) {
	t.Parallel()

	committed := ""
	store := &mockCommitStore{text: &committed}
	registry := capabilities.NewRegistry()
	registry.Register("stability_api", fakeCapabilityErrorAdapter{
		name: "stability_api",
		tools: []gateway.ToolDefinition{{
			Name:        "generate",
			Description: "generate image",
		}},
		err: errors.New("adapter unavailable"),
	})

	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{Capabilities: registry})
	w.capabilityRouter = enabledCapabilityRouter()

	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-cap-err"},
		Title:       "Generate an image",
		Description: "Draw a picture of a cat",
	}
	profile := models.AgentProfile{ID: "agent-1"}

	_, ok, err := w.tryExternalCapabilityRoute(context.Background(), task, models.Project{}, profile, &messages)
	if err == nil {
		t.Fatal("tryExternalCapabilityRoute() err = nil, want terminal error after failHard")
	}
	if ok {
		t.Fatal("tryExternalCapabilityRoute() ok = true, want false")
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2 (no assistant append)", len(messages))
	}
	if strings.Contains(committed, `"tool":"generate"`) {
		t.Fatalf("committed = %q, want no adapter success payload", committed)
	}
	if !strings.Contains(committed, "capability routing") {
		t.Fatalf("committed = %q, want failHard error payload", committed)
	}
}

func TestProcessAgentic_ExternalCapabilityRoute_CallToolError_NoAgenticTurns(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)

	registry := capabilities.NewRegistry()
	registry.Register("stability_api", fakeCapabilityErrorAdapter{
		name: "stability_api",
		tools: []gateway.ToolDefinition{{
			Name:        "generate",
			Description: "generate",
		}},
		err: errors.New("adapter unavailable"),
	})
	w.capabilities = registry
	w.capabilityRouter = enabledCapabilityRouter()

	store.task.Title = "Generate an image"
	store.task.Description = "Draw a picture logo illustration"

	w.Process(context.Background(), store.task)

	for _, req := range gw.requests {
		if !req.JSONMode {
			t.Fatalf("expected no agentic gateway turns after capability hard-fail, got request: %+v", req)
		}
	}
	if store.result == nil || store.result.Success {
		t.Fatalf("result = %+v, want failed task result", store.result)
	}
}

func TestProcessAgentic_ExternalCapabilityRoute_SkipsGateway(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)

	registry := capabilities.NewRegistry()
	registry.Register("stability_api", fakeCapabilityCallAdapter{
		name: "stability_api",
		tools: []gateway.ToolDefinition{{
			Name:        "generate",
			Description: "generate",
		}},
	})
	w.capabilities = registry
	w.capabilityRouter = enabledCapabilityRouter()

	store.task.Title = "Generate an image"
	store.task.Description = "Draw a picture logo illustration"

	w.Process(context.Background(), store.task)

	for _, req := range gw.requests {
		if !req.JSONMode {
			t.Fatalf("expected no agentic gateway turns, got request: %+v", req)
		}
	}
	if store.result == nil || store.result.Payload == "" {
		t.Fatal("expected task result to be committed")
	}
}

func TestProcessAgentic_ExternalCapabilityRoute_MissingAdapterFallsThrough(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)
	w.capabilityRouter = enabledCapabilityRouter()
	w.capabilities = capabilities.NewRegistry()

	store.task.Title = "Generate an image"
	store.task.Description = "Draw a picture logo illustration"

	w.Process(context.Background(), store.task)

	var agenticTurn bool
	for _, req := range gw.requests {
		if !req.JSONMode {
			agenticTurn = true
			break
		}
	}
	if !agenticTurn {
		t.Fatal("expected agentic gateway turn when adapter is missing")
	}
}

func TestProcessAgentic_UnmatchedCapabilityIntentContinuesLoop(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)
	w.capabilityRouter = enabledCapabilityRouter()
	store.task.Description = testutil.AgenticTestTaskDescription()

	w.Process(context.Background(), store.task)

	var agenticTurn bool
	for _, req := range gw.requests {
		if !req.JSONMode {
			agenticTurn = true
			break
		}
	}
	if !agenticTurn {
		t.Fatal("expected agentic gateway turn for unmatched intent")
	}
}

func TestEncodeCapabilityResult(t *testing.T) {
	t.Parallel()
	text, err := encodeCapabilityResult(map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["ok"] != true {
		t.Fatalf("parsed = %v", parsed)
	}
	s, err := encodeCapabilityResult("plain")
	if err != nil || s != "plain" {
		t.Fatalf("string result = %q err=%v", s, err)
	}
}
