package worker

import (
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func testModelRoutingConfig() config.ModelRoutingConfig {
	return config.ModelRoutingConfig{
		Enabled:               true,
		ContextTokenThreshold: 150000,
		Cheap:                 config.ModelTierTarget{Provider: "anthropic", Model: "claude-haiku"},
		Mid:                   config.ModelTierTarget{Provider: "anthropic", Model: "claude-sonnet"},
		High:                  config.ModelTierTarget{Provider: "anthropic", Model: "claude-opus"},
	}
}

func TestComplexityScorer_SummarizeLowScore(t *testing.T) {
	t.Parallel()
	task := models.Task{Description: "summarize this file"}
	score := (ComplexityScorer{}).ScoreTask(task)
	if score > 2 {
		t.Fatalf("ScoreTask() = %d, want <= 2", score)
	}
}

func TestComplexityScorer_ArchitectHighScore(t *testing.T) {
	t.Parallel()
	// Spec reasoning keywords combined with the acceptance architecture phrase.
	task := models.Task{Description: "architect compare design analyse a microservice migration strategy"}
	score := (ComplexityScorer{}).ScoreTask(task)
	if score < 7 {
		t.Fatalf("ScoreTask() = %d, want >= 7", score)
	}
}

func TestComplexityScorer_ArchitectPhraseAloneIsLow(t *testing.T) {
	t.Parallel()
	task := models.Task{Description: "architect a microservice migration strategy"}
	score := (ComplexityScorer{}).ScoreTask(task)
	if score > 2 {
		t.Fatalf("ScoreTask() = %d, want <= 2 with only architect reasoning signal", score)
	}
}

func TestComplexityScorer_NoSubstringFalsePositives(t *testing.T) {
	t.Parallel()
	task := models.Task{Description: "listening to information while rewriting"}
	score := (ComplexityScorer{}).ScoreTask(task)
	if score != 0 {
		t.Fatalf("ScoreTask() = %d, want 0 (no substring keyword matches)", score)
	}
}

func TestComplexityScorer_Clamped(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("architect reason compare design analyse ")
	}
	score := (ComplexityScorer{}).ScoreTask(models.Task{Description: b.String()})
	if score != 10 {
		t.Fatalf("ScoreTask() = %d, want 10 (clamped)", score)
	}
}

func TestModelRouter_SummarizeRoutesCheap(t *testing.T) {
	t.Parallel()
	r := NewModelRouter(testModelRoutingConfig())
	task := models.Task{Description: "summarize this file"}
	provider, model, ok := r.Route(task, 0)
	if !ok {
		t.Fatal("Route() ok = false, want true")
	}
	if provider != "anthropic" || model != "claude-haiku" {
		t.Fatalf("Route() = %q/%q, want anthropic/claude-haiku", provider, model)
	}
}

func TestModelRouter_ArchitectRoutesHigh(t *testing.T) {
	t.Parallel()
	r := NewModelRouter(testModelRoutingConfig())
	task := models.Task{Description: "architect compare design analyse a microservice migration strategy"}
	_, model, ok := r.Route(task, 0)
	if !ok {
		t.Fatal("Route() ok = false, want true")
	}
	if model != "claude-opus" {
		t.Fatalf("Route() model = %q, want claude-opus", model)
	}
}

func TestModelRouter_ContextOverrideForcesHigh(t *testing.T) {
	t.Parallel()
	r := NewModelRouter(testModelRoutingConfig())
	task := models.Task{Description: "summarize this file"}
	provider, model, ok := r.Route(task, 150000)
	if !ok {
		t.Fatal("Route() ok = false, want true")
	}
	if model != "claude-opus" {
		t.Fatalf("Route() model = %q, want claude-opus despite low score", model)
	}
	if provider != "anthropic" {
		t.Fatalf("Route() provider = %q, want anthropic", provider)
	}
}

func TestModelRouter_PartialTierFallback(t *testing.T) {
	t.Parallel()
	cfg := config.ModelRoutingConfig{
		Enabled:               true,
		ContextTokenThreshold: 150000,
		High:                  config.ModelTierTarget{Provider: "anthropic", Model: "claude-opus"},
	}
	r := NewModelRouter(cfg)
	if r == nil {
		t.Fatal("NewModelRouter() = nil, want router with only high tier")
	}
	task := models.Task{Description: "summarize this file"}
	provider, model, ok := r.Route(task, 0)
	if !ok {
		t.Fatal("Route() ok = false, want true")
	}
	if provider != "anthropic" || model != "claude-opus" {
		t.Fatalf("Route() = %q/%q, want anthropic/claude-opus fallback", provider, model)
	}
}

func TestModelRouter_PartialTierMidOnly(t *testing.T) {
	t.Parallel()
	cfg := config.ModelRoutingConfig{
		Enabled:               true,
		ContextTokenThreshold: 150000,
		Mid:                   config.ModelTierTarget{Provider: "anthropic", Model: "claude-sonnet"},
	}
	r := NewModelRouter(cfg)
	task := models.Task{Description: "architect compare design analyse migration"}
	_, model, ok := r.Route(task, 0)
	if !ok {
		t.Fatal("Route() ok = false, want true")
	}
	if model != "claude-sonnet" {
		t.Fatalf("Route() model = %q, want claude-sonnet fallback from unset high", model)
	}
}

func TestEstimateContextTokens_LargeContext(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("x", 600001) // 600001/4 > 150000
	messages := []gateway.PromptMessage{{Role: "user", Content: content}}
	tokens := EstimateContextTokens(messages, nil)
	if tokens < 150000 {
		t.Fatalf("EstimateContextTokens() = %d, want >= 150000", tokens)
	}
}

func TestTotalToolChars_NoDoubleCount(t *testing.T) {
	t.Parallel()
	tool := gateway.ToolDefinition{
		Name:        "run_command",
		Description: "Execute a shell command",
	}
	b, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("json.Marshal(tool): %v", err)
	}
	got := totalToolChars([]gateway.ToolDefinition{tool})
	if got != len(b) {
		t.Fatalf("totalToolChars() = %d, want %d (marshaled JSON only)", got, len(b))
	}
	oldStyle := len(tool.Name) + len(tool.Description) + len(b)
	if got >= oldStyle {
		t.Fatalf("totalToolChars() = %d, want strictly less than double-count %d", got, oldStyle)
	}
}

func TestEstimateContextTokens_IncludesTools(t *testing.T) {
	t.Parallel()
	tools := []gateway.ToolDefinition{{
		Name:        "run_command",
		Description: strings.Repeat("x", 600001),
	}}
	tokens := EstimateContextTokens(nil, tools)
	if tokens < 150000 {
		t.Fatalf("EstimateContextTokens() = %d, want >= 150000 from tool defs", tokens)
	}
}

func TestNewModelRouter_DisabledOrEmptyTiers(t *testing.T) {
	t.Parallel()
	if NewModelRouter(config.ModelRoutingConfig{Enabled: false}) != nil {
		t.Fatal("disabled config should return nil router")
	}
	cfg := config.ModelRoutingConfig{Enabled: true}
	if NewModelRouter(cfg) != nil {
		t.Fatal("enabled with no tier targets should return nil router")
	}
	cfg = config.ModelRoutingConfig{
		Enabled: true,
		High:    config.ModelTierTarget{Provider: "anthropic", Model: "claude-opus"},
	}
	if NewModelRouter(cfg) == nil {
		t.Fatal("enabled with partial high tier should return non-nil router")
	}
}

func TestApplyModelRouting_OverridesProfileModel(t *testing.T) {
	t.Parallel()
	w := &Worker{
		modelRouter: NewModelRouter(testModelRoutingConfig()),
	}
	profile := models.AgentProfile{Provider: "openai", Model: "gpt-4"}
	task := models.Task{Description: "summarize this file"}
	got := w.applyModelRouting(task, profile, nil, nil)
	if got.Model != "claude-haiku" || got.Provider != "anthropic" {
		t.Fatalf("applyModelRouting() = %+v, want anthropic/claude-haiku", got)
	}
}

func TestApplyModelRouting_UnpinnedRoutesCheap(t *testing.T) {
	t.Parallel()
	w := &Worker{
		modelRouter: NewModelRouter(testModelRoutingConfig()),
	}
	profile := models.AgentProfile{Provider: "openai"}
	task := models.Task{Description: "summarize this file"}
	got := w.applyModelRouting(task, profile, nil, nil)
	if got.Model != "claude-haiku" || got.Provider != "anthropic" {
		t.Fatalf("applyModelRouting() = %+v, want anthropic/claude-haiku", got)
	}
}

func TestApplyModelRouting_ContextOverrideHigh(t *testing.T) {
	t.Parallel()
	w := &Worker{
		modelRouter: NewModelRouter(testModelRoutingConfig()),
	}
	profile := models.AgentProfile{}
	task := models.Task{Description: "summarize this file"}
	content := strings.Repeat("x", 600001)
	messages := []gateway.PromptMessage{{Role: "user", Content: content}}
	got := w.applyModelRouting(task, profile, messages, nil)
	if got.Model != "claude-opus" {
		t.Fatalf("applyModelRouting() model = %q, want claude-opus", got.Model)
	}
}

// TestApplyModelRouting_UsesFullToolsBeforeManifestFilter verifies that model routing
// token estimates use the pre-filter tool registry while the turn loop uses manifest-filtered tools.
func TestApplyModelRouting_UsesFullToolsBeforeManifestFilter(t *testing.T) {
	t.Parallel()
	w := &Worker{
		modelRouter:  NewModelRouter(testModelRoutingConfig()),
		toolManifest: enabledToolManifest(),
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t-route-manifest"},
		Title:       "Summarize weekly report",
		Description: "Provide a short recap and condense into bullet points",
	}
	profile := models.AgentProfile{Provider: "openai", Model: "gpt-4"}
	routingTools := []gateway.ToolDefinition{{
		Name:        "run_command",
		Description: strings.Repeat("x", 600001),
	}}
	filtered, _ := w.filterAgenticTools(routingTools, nil, task, profile)
	if len(filtered) != 0 {
		t.Fatalf("filtered tools len = %d, want 0 for summarize manifest", len(filtered))
	}
	if tokens := EstimateContextTokens(nil, filtered); tokens >= 150000 {
		t.Fatalf("filtered EstimateContextTokens() = %d, want below threshold", tokens)
	}

	routedFull := w.applyModelRouting(task, profile, nil, routingTools)
	if routedFull.Model != "claude-opus" || routedFull.Provider != "anthropic" {
		t.Fatalf("routing with full tools = %s/%s, want anthropic/claude-opus", routedFull.Provider, routedFull.Model)
	}
	routedFiltered := w.applyModelRouting(task, profile, nil, filtered)
	if routedFiltered.Model != "claude-haiku" || routedFiltered.Provider != "anthropic" {
		t.Fatalf("routing with filtered tools = %s/%s, want anthropic/claude-haiku", routedFiltered.Provider, routedFiltered.Model)
	}
}
