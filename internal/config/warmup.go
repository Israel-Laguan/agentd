package config

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"agentd/internal/gateway"
)

// WarmupLLM confirms that the active LLM provider is reachable and can respond
// to a real request. For async providers (Horde), a lightweight HTTP heartbeat
// is used instead of a full generation round-trip.
//
// Returns an error if the provider is unreachable or returns an error response,
// which will block daemon startup.
func WarmupLLM(ctx context.Context, gw gateway.AIGateway, gatewayCfg GatewayConfig) error {
	result := CheckProviders(gatewayCfg)
	if !result.Available {
		return fmt.Errorf("no LLM provider available for warmup")
	}

	// Horde is async — a generate call can take minutes. Ping its heartbeat instead.
	if result.HealthMode == healthModeHorde {
		return warmupHorde(ctx, result.Provider, result.BaseURL)
	}

	slog.Debug("LLM warmup starting", "provider", result.Provider)
	warmupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	resp, err := gw.Generate(warmupCtx, gateway.AIRequest{
		Messages:  []gateway.PromptMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 64,
	})
	if err != nil {
		return fmt.Errorf("LLM warmup failed (provider=%s): %w", result.Provider, err)
	}
	slog.Info("LLM warmup OK", "provider", resp.ProviderUsed, "model", resp.ModelUsed)
	return nil
}

// warmupHorde pings the AI Horde heartbeat endpoint to confirm the service is
// reachable without queuing a full async generation job.
func warmupHorde(ctx context.Context, provider, baseURL string) error {
	slog.Debug("LLM warmup starting", "provider", provider, "mode", "heartbeat")
	client := &http.Client{Timeout: 5 * time.Second}
	url := baseURL + "/v2/status/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("horde warmup request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("horde warmup failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("horde heartbeat returned %d", resp.StatusCode)
	}
	slog.Info("LLM warmup OK", "provider", provider, "mode", "heartbeat")
	return nil
}
