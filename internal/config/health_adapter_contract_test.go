package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/gateway"
)

// healthAdapterContract describes default health-probe behavior per adapter type.
type healthAdapterContract struct {
	adapter    string
	synthName  string
	healthMode string
	probePath  string // empty when probe is api_key only (no HTTP GET)
}

var healthAdapterContracts = map[string]healthAdapterContract{
	"openai":    {adapter: "openai", synthName: "synth-health-openai", healthMode: healthModeAPIKey},
	"anthropic": {adapter: "anthropic", synthName: "synth-health-anthropic", healthMode: healthModeAPIKey},
	"ollama":    {adapter: "ollama", synthName: "synth-health-ollama", healthMode: healthModeOllama, probePath: "/api/tags"},
	"llamacpp":  {adapter: "llamacpp", synthName: "synth-health-llamacpp", healthMode: healthModeLlamaCpp, probePath: "/v1/models"},
	"horde":     {adapter: "horde", synthName: "synth-health-horde", healthMode: healthModeHorde, probePath: "/v2/status/heartbeat"},
}

func runHealthAdapterContract(t *testing.T, contract healthAdapterContract) {
	t.Helper()
	t.Run("default_health_mode", func(t *testing.T) {
		t.Parallel()
		testHealthAdapterDefaultMode(t, contract)
	})
	if contract.probePath != "" {
		t.Run("probe_path", func(t *testing.T) {
			t.Parallel()
			testHealthAdapterProbePath(t, contract)
		})
	}
}

func TestOpenAIAdapterHealthContract(t *testing.T) {
	t.Parallel()
	runHealthAdapterContract(t, healthAdapterContracts["openai"])
}

func TestAnthropicAdapterHealthContract(t *testing.T) {
	t.Parallel()
	runHealthAdapterContract(t, healthAdapterContracts["anthropic"])
}

func TestOllamaAdapterHealthContract(t *testing.T) {
	t.Parallel()
	runHealthAdapterContract(t, healthAdapterContracts["ollama"])
}

func TestLlamaCppAdapterHealthContract(t *testing.T) {
	t.Parallel()
	runHealthAdapterContract(t, healthAdapterContracts["llamacpp"])
}

func TestHordeAdapterHealthContract(t *testing.T) {
	t.Parallel()
	runHealthAdapterContract(t, healthAdapterContracts["horde"])
}

// TestGeminiAdapterAliasHealthMode verifies healthModeFor before adapter canonicalization.
// Legacy gemini adapter alias is the only vendor-specific health case; see adapter_contract_test.go.
func TestGeminiAdapterAliasHealthMode(t *testing.T) {
	t.Parallel()

	p := gateway.ProviderConfig{
		Name:    "gemini",
		Adapter: "gemini",
		APIKey:  "",
	}
	if got := healthModeFor(p); got != healthModeAPIKey {
		t.Errorf("healthModeFor(adapter=gemini) = %q, want %q", got, healthModeAPIKey)
	}
}

func testHealthAdapterDefaultMode(t *testing.T, contract healthAdapterContract) {
	t.Helper()

	p := gateway.ProviderConfig{
		Name:    contract.synthName,
		Adapter: contract.adapter,
		APIKey:  "contract-key",
		Model:   "contract-model",
	}
	if got := healthModeFor(p); got != contract.healthMode {
		t.Fatalf("healthModeFor() = %q, want %q", got, contract.healthMode)
	}
}

func testHealthAdapterProbePath(t *testing.T, contract healthAdapterContract) {
	t.Helper()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := GatewayConfig{
		Order: []string{contract.synthName},
		Providers: []gateway.ProviderConfig{{
			Name:    contract.synthName,
			Adapter: contract.adapter,
			BaseURL: srv.URL,
			Model:   "contract-model",
			APIKey:  "0000000000",
		}},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Fatal("expected Available=true for healthy probe")
	}
	if gotPath != contract.probePath {
		t.Fatalf("probe path = %q, want %q", gotPath, contract.probePath)
	}
	if result.HealthMode != contract.healthMode {
		t.Fatalf("HealthMode = %q, want %q", result.HealthMode, contract.healthMode)
	}
}
