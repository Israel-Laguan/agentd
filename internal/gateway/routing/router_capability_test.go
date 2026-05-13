package routing

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

// mockProvider implements providers.Backend for capability tests
type mockProvider struct {
	providerName string
	budget       int
	request      spec.AIRequest
	capabilities providers.Capabilities
}

func (p *mockProvider) Name() spec.Provider        { return spec.Provider(p.providerName) }
func (p *mockProvider) MaxInputChars() int         { return p.budget }
func (p *mockProvider) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	p.request = req
	return spec.AIResponse{Content: "ok", ProviderUsed: string(p.providerName)}, nil
}
func (p *mockProvider) Capabilities() providers.Capabilities {
	return p.capabilities
}

// TestExplicitProviderUnsupportedToolsError tests that when a provider is explicitly
// requested and doesn't support tools, an error is returned with the provider name
// and "tools not supported" message.
func TestExplicitProviderUnsupportedToolsError(t *testing.T) {
	// Create a provider that doesn't support tools
	provider := &mockProvider{
		providerName: "unsupported",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: false},
	}

	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000)

	// Request with explicit provider that doesn't support tools
	req := spec.AIRequest{
		Provider: "unsupported",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		Tools:    []spec.ToolDefinition{{Name: "test_tool"}}, // Non-empty tools
	}

	_, err := router.Generate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for explicit unsupported provider, got nil")
	}

	// Check error message contains provider name and "tools"
	errMsg := err.Error()
	if !strings.Contains(errMsg, "unsupported") {
		t.Errorf("error should contain provider name 'unsupported', got: %v", err)
	}
	if !strings.Contains(errMsg, "tools") {
		t.Errorf("error should contain 'tools', got: %v", err)
	}
	if !strings.Contains(errMsg, "disable agentic mode") && !strings.Contains(errMsg, "different provider") {
		t.Errorf("error should suggest using different provider or disabling agentic mode, got: %v", err)
	}
}

// TestOpenAIProviderWithTools tests that OpenAI provider (supports tools) works correctly
func TestOpenAIProviderWithTools(t *testing.T) {
	provider := &mockProvider{
		providerName: "openai",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: true},
	}

	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000)

	req := spec.AIRequest{
		Provider: "openai",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		Tools:    []spec.ToolDefinition{{Name: "test_tool"}},
	}

	resp, err := router.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tools were passed through to the provider
	if len(provider.request.Tools) != 1 {
		t.Errorf("expected 1 tool in request, got %d", len(provider.request.Tools))
	}

	// Verify response is valid
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got: %s", resp.Content)
	}
}

// TestOllamaProviderWithToolsFallback tests that non-OpenAI providers fall back
// to legacy JSON mode when tools are provided but provider doesn't support them
// (this is for the case when provider is NOT explicitly specified)
func TestOllamaProviderWithToolsFallback(t *testing.T) {
	provider := &mockProvider{
		providerName: "ollama",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: false},
	}

	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000)

	// Request without explicit provider - should fallback to legacy mode
	req := spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		Tools:    []spec.ToolDefinition{{Name: "test_tool"}},
	}

	resp, err := router.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the request was converted to JSON mode
	if !provider.request.JSONMode {
		t.Error("expected JSONMode to be true in fallback request")
	}

	// Verify tools were cleared in the request to provider
	if len(provider.request.Tools) != 0 {
		t.Errorf("expected tools to be cleared in fallback request, got %d tools", len(provider.request.Tools))
	}

	// Verify response is valid
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got: %s", resp.Content)
	}
}

// TestExplicitProviderWithToolsSupport tests that explicit provider with tools support works
func TestExplicitProviderWithToolsSupport(t *testing.T) {
	provider := &mockProvider{
		providerName: "openai",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: true},
	}

	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000)

	req := spec.AIRequest{
		Provider: "openai",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		Tools:    []spec.ToolDefinition{{Name: "test_tool"}},
	}

	resp, err := router.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tools were passed through
	if len(provider.request.Tools) != 1 {
		t.Errorf("expected 1 tool in request, got %d", len(provider.request.Tools))
	}

	// Verify JSONMode is not set
	if provider.request.JSONMode {
		t.Error("expected JSONMode to be false when provider supports tools")
	}

	// Verify response is valid
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got: %s", resp.Content)
	}
}

// TestProviderWithoutExplicitAndNoTools tests that provider without explicit selection
// and no tools works normally
func TestProviderWithoutExplicitAndNoTools(t *testing.T) {
	provider := &mockProvider{
		providerName: "ollama",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: false},
	}

	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000)

	req := spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		// No Tools field set
	}

	resp, err := router.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify response is valid
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got: %s", resp.Content)
	}
}

// TestAnthropicProviderWithToolsSupport tests that Anthropic provider (supports tools) works correctly
func TestAnthropicProviderWithToolsSupport(t *testing.T) {
	provider := &mockProvider{
		providerName: "anthropic",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: true},
	}

	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}, 12000)

	req := spec.AIRequest{
		Provider: "anthropic",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
		Tools:    []spec.ToolDefinition{{Name: "test_tool"}},
	}

	resp, err := router.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tools were passed through to the provider
	if len(provider.request.Tools) != 1 {
		t.Errorf("expected 1 tool in request, got %d", len(provider.request.Tools))
	}

	// Verify response is valid
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got: %s", resp.Content)
	}
}