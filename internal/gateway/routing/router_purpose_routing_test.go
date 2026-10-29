package routing

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

func purposeTestTable() route.Table {
	return route.Table{
		Agents: map[string]route.Agent{
			"quick": {Provider: "gemini", Model: "flash", Level: spec.LevelL1},
		},
		Stages: map[spec.Purpose]route.StageRoute{
			spec.PurposeCreateElicit: {Default: "quick"},
			spec.PurposeExecWorkPlan: {
				Default: "",
				Rules: []route.Rule{
					{Match: route.Match{RequiresWorkspaceChange: boolPtr(true)}, Use: "quick"},
				},
			},
		},
	}
}

func boolPtr(b bool) *bool { return &b }

// purposeProviders is a cascade of openai first, then gemini: a call that is not
// routed to gemini lands on openai.
func purposeProviders() (*mockProvider, *mockProvider) {
	openai := &mockProvider{providerName: "openai", budget: 10000, content: "ok", capabilities: providers.Capabilities{SupportsChatTools: true}}
	gemini := &mockProvider{providerName: "gemini", budget: 10000, content: "ok", capabilities: providers.Capabilities{SupportsChatTools: true}}
	return openai, gemini
}

func TestPurposeRoutingStageDefaultInjectsProviderAndModel(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "gemini" {
		t.Fatalf("ProviderUsed = %q, want gemini from the stage default", resp.ProviderUsed)
	}
	if gemini.request.Model != "flash" {
		t.Fatalf("model = %q, want flash from the chosen agent", gemini.request.Model)
	}
}

// Rule order is pinned in the route package. Here the rule must reach the call:
// a task that requires a workspace change takes the rule, another takes the
// stage default (no default, so the cascade).
func TestPurposeRoutingRuleRunsOnlyWhenItsConditionHolds(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	base := spec.AIRequest{
		Purpose:  spec.PurposeExecWorkPlan,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	}
	withoutChange := base
	resp, err := router.Generate(context.Background(), withoutChange)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "openai" {
		t.Fatalf("no-change task ProviderUsed = %q, want the cascade (openai)", resp.ProviderUsed)
	}

	withChange := base
	withChange.Attrs = spec.TaskAttrs{RequiresWorkspaceChange: boolPtr(true)}
	resp, err = router.Generate(context.Background(), withChange)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "gemini" {
		t.Fatalf("change task ProviderUsed = %q, want gemini from the rule", resp.ProviderUsed)
	}
}

func TestPurposeRoutingAgentIDIsAPin(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		AgentID:  "tier-context",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "openai" {
		t.Fatalf("pinned call ProviderUsed = %q, want the cascade (openai): a pin injects nothing", resp.ProviderUsed)
	}
}

func TestPurposeRoutingSeededDefaultProfileIsNotAPin(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		AgentID:  route.DefaultAgentID,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "gemini" {
		t.Fatalf("ProviderUsed = %q, want gemini: the seeded default must not pin", resp.ProviderUsed)
	}
}

func TestPurposeRoutingExplicitProviderOverridesRoute(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Provider: "openai",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "openai" {
		t.Fatalf("explicit Provider should override the route, got %q", resp.ProviderUsed)
	}
}

func TestPurposeRoutingCascadesToNextOnFailure(t *testing.T) {
	openai, gemini := purposeProviders()
	gemini.err = errors.New("gemini quota exhausted")
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v, want the cascade to reach openai", err)
	}
	if resp.ProviderUsed != "openai" {
		t.Fatalf("ProviderUsed = %q, want openai after gemini failed", resp.ProviderUsed)
	}
}

func TestPurposeRoutingWhitespaceProviderUsesRoute(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Provider: "   ",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "gemini" {
		t.Fatalf("ProviderUsed = %q, want gemini: whitespace is unspecified", resp.ProviderUsed)
	}
}

// A routed provider without tool support still gets JSON fallback when tools are
// sent, as the old role-mapped provider did.
func TestPurposeRoutingNonToolProviderWithToolsJSONFallback(t *testing.T) {
	plain := &mockProvider{providerName: "gemini", budget: 10000, content: "ok", capabilities: providers.Capabilities{SupportsChatTools: false}}
	table := route.Table{
		Agents: map[string]route.Agent{"quick": {Provider: "gemini", Model: "flash", Level: spec.LevelL1}},
		Stages: map[spec.Purpose]route.StageRoute{spec.PurposeCreateElicit: {Default: "quick"}},
	}
	router := NewRouter(plain).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000).WithPurposeRouting(table)

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		Tools:    []spec.ToolDefinition{{Name: "test_tool"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if !plain.request.JSONMode {
		t.Error("expected JSON fallback for a routed non-tool provider with tools")
	}
	if len(plain.request.Tools) != 0 {
		t.Errorf("expected tools cleared, got %d", len(plain.request.Tools))
	}
}

func TestPurposeRoutingEmptyTableKeepsTheCascade(t *testing.T) {
	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(route.Table{})

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "openai" {
		t.Fatalf("ProviderUsed = %q, want openai: an empty table routes nothing", resp.ProviderUsed)
	}
}

// Every decision is logged once, with the purpose, the required level, the rule
// and the agent chosen.
func TestPurposeRoutingLogsOneEventPerDecision(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	openai, gemini := purposeProviders()
	router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable())
	if _, err := router.Generate(context.Background(), spec.AIRequest{
		Purpose:  spec.PurposeCreateElicit,
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var routing []string
	for _, line := range lines {
		if strings.Contains(line, "routing decision") {
			routing = append(routing, line)
		}
	}
	if len(routing) != 1 {
		t.Fatalf("routing events = %d, want exactly 1:\n%s", len(routing), buf.String())
	}
	for _, want := range []string{"purpose=create.elicit", "required_level=L1", "rule=default", "agent=quick"} {
		if !strings.Contains(routing[0], want) {
			t.Fatalf("routing event %q is missing %q", routing[0], want)
		}
	}
}
