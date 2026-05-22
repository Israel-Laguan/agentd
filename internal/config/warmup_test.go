package config

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

type mockWarmupGateway struct {
	generate func(context.Context, gateway.AIRequest) (gateway.AIResponse, error)
}

func (m *mockWarmupGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if m.generate != nil {
		return m.generate(ctx, req)
	}
	return gateway.AIResponse{}, nil
}

func (m *mockWarmupGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func (m *mockWarmupGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (m *mockWarmupGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (m *mockWarmupGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func TestWarmupLLM_HordeHeartbeatOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/status/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := GatewayConfig{
		Order: []string{"horde"},
		Horde: gateway.ProviderConfig{BaseURL: srv.URL},
	}
	if err := WarmupLLM(context.Background(), &mockWarmupGateway{}, cfg); err != nil {
		t.Fatalf("WarmupLLM() error = %v", err)
	}
}

func TestWarmupLLM_HordeHeartbeatNonOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := GatewayConfig{
		Order: []string{"horde"},
		Horde: gateway.ProviderConfig{BaseURL: srv.URL},
	}
	err := WarmupLLM(context.Background(), &mockWarmupGateway{}, cfg)
	if err == nil {
		t.Fatal("expected error for non-OK heartbeat")
	}
}

func TestWarmupLLM_HordeHeartbeatNetworkError(t *testing.T) {
	t.Parallel()
	cfg := GatewayConfig{
		Order: []string{"horde"},
		Horde: gateway.ProviderConfig{BaseURL: "http://127.0.0.1:1"},
	}
	err := WarmupLLM(context.Background(), &mockWarmupGateway{}, cfg)
	if err == nil {
		t.Fatal("expected error for unreachable horde endpoint")
	}
}

func TestWarmupLLM_GenerateOK(t *testing.T) {
	t.Parallel()
	gw := &mockWarmupGateway{
		generate: func(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
			return gateway.AIResponse{Content: "ok", ProviderUsed: "openai", ModelUsed: "gpt-4o-mini"}, nil
		},
	}
	cfg := GatewayConfig{
		Order:  []string{"openai"},
		OpenAI: gateway.ProviderConfig{APIKey: "sk-test", Model: "gpt-4o-mini"},
	}
	if err := WarmupLLM(context.Background(), gw, cfg); err != nil {
		t.Fatalf("WarmupLLM() error = %v", err)
	}
}

func TestWarmupLLM_GenerateFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("provider down")
	gw := &mockWarmupGateway{
		generate: func(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
			return gateway.AIResponse{}, want
		},
	}
	cfg := GatewayConfig{
		Order:  []string{"openai"},
		OpenAI: gateway.ProviderConfig{APIKey: "sk-test", Model: "gpt-4o-mini"},
	}
	err := WarmupLLM(context.Background(), gw, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, want) {
		t.Fatalf("WarmupLLM() error = %v, want %v", err, want)
	}
}

func TestWarmupLLM_NoProvider(t *testing.T) {
	t.Parallel()
	err := WarmupLLM(context.Background(), &mockWarmupGateway{}, GatewayConfig{})
	if err == nil {
		t.Fatal("expected error when no provider is available")
	}
}
