package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway/spec"
)

// adapterContract describes wire-protocol invariants for one adapter type.
// Synthetic provider names are used so new vendor aliases do not require new tests.
type adapterContract struct {
	adapter       string
	synthName     string
	wantChatTools bool
	generatePath  string
}

var adapterContracts = map[string]adapterContract{
	"openai": {
		adapter: "openai", synthName: "synthetic-openai", wantChatTools: true,
		generatePath: "/v1/chat/completions",
	},
	"anthropic": {
		adapter: "anthropic", synthName: "synthetic-anthropic", wantChatTools: true,
		generatePath: "/v1/messages",
	},
	"ollama": {
		adapter: "ollama", synthName: "synthetic-ollama", wantChatTools: false,
		generatePath: "/api/chat",
	},
	"llamacpp": {
		adapter: "llamacpp", synthName: "synthetic-llamacpp", wantChatTools: false,
		generatePath: "/v1/chat/completions",
	},
	"horde": {
		adapter: "horde", synthName: "synthetic-horde", wantChatTools: false,
		generatePath: "/v2/generate/text/async",
	},
}

func runAdapterContract(t *testing.T, adapter string) {
	t.Helper()
	contract, ok := adapterContracts[adapter]
	if !ok {
		t.Fatalf("unknown adapter contract %q", adapter)
	}
	t.Run("capabilities", func(t *testing.T) {
		testAdapterContractCapabilities(t, contract)
	})
	t.Run("synthetic_name_registration", func(t *testing.T) {
		testAdapterContractSyntheticRegistration(t, contract)
	})
	t.Run("http_wire_path", func(t *testing.T) {
		testAdapterContractGeneratePath(t, contract)
	})
	if contract.wantChatTools {
		t.Run("tool_format_on_wire", func(t *testing.T) {
			testAdapterContractToolFormat(t, contract)
		})
	}
}

func TestOpenAIAdapterContract(t *testing.T) {
	t.Parallel()
	runAdapterContract(t, "openai")
	t.Run("legacy_gemini_alias", func(t *testing.T) {
		t.Parallel()
		testOpenAIContractLegacyGeminiAlias(t)
	})
}

func TestAnthropicAdapterContract(t *testing.T) {
	t.Parallel()
	runAdapterContract(t, "anthropic")
}

func TestOllamaAdapterContract(t *testing.T) {
	t.Parallel()
	runAdapterContract(t, "ollama")
}

func TestLlamaCppAdapterContract(t *testing.T) {
	t.Parallel()
	runAdapterContract(t, "llamacpp")
}

func TestHordeAdapterContract(t *testing.T) {
	t.Parallel()
	runAdapterContract(t, "horde")
}

func testAdapterContractCapabilities(t *testing.T, contract adapterContract) {
	t.Helper()
	t.Parallel()

	backends, err := AppendFromConfig(nil, spec.ProviderConfig{
		Name:    contract.synthName,
		Adapter: contract.adapter,
		BaseURL: "http://example.invalid",
		Model:   "contract-model",
	})
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("len = %d, want 1", len(backends))
	}
	got := backends[0].Capabilities().SupportsChatTools
	if got != contract.wantChatTools {
		t.Fatalf("SupportsChatTools = %v, want %v", got, contract.wantChatTools)
	}
}

func testAdapterContractSyntheticRegistration(t *testing.T, contract adapterContract) {
	t.Helper()
	t.Parallel()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{
		Name:    contract.synthName,
		Adapter: contract.adapter,
		BaseURL: "http://example.invalid",
	})
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name() != spec.Provider(contract.synthName) {
		t.Fatalf("Name() = %q, want %q", got[0].Name(), contract.synthName)
	}
}

func testAdapterContractGeneratePath(t *testing.T, contract adapterContract) {
	t.Helper()
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotPath == "" {
			gotPath = r.URL.Path
		}
		writeAdapterContractGenerateResponse(t, w, r, contract.adapter)
	}))
	defer srv.Close()

	backend := newAdapterContractBackend(t, contract, srv)
	_, err := backend.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if gotPath != contract.generatePath {
		t.Fatalf("HTTP path = %q, want %q", gotPath, contract.generatePath)
	}
}

func testAdapterContractToolFormat(t *testing.T, contract adapterContract) {
	t.Helper()
	t.Parallel()

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeAdapterContractGenerateResponse(t, w, r, contract.adapter)
	}))
	defer srv.Close()

	backend := newAdapterContractBackend(t, contract, srv)
	tool := spec.ToolDefinition{Name: "contract_tool", Description: "probe", Parameters: &spec.FunctionParameters{}}
	_, err := backend.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "use tool"}},
		Tools:    []spec.ToolDefinition{tool},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	assertAdapterContractToolFormat(t, contract.adapter, body)
}

func newAdapterContractBackend(t *testing.T, contract adapterContract, srv *httptest.Server) Backend {
	t.Helper()

	cfg := spec.ProviderConfig{
		Name:    contract.synthName,
		Adapter: contract.adapter,
		BaseURL: srv.URL,
		Model:   "contract-model",
		APIKey:  "contract-key",
	}
	if contract.adapter == "openai" {
		cfg.BaseURL = srv.URL + "/v1"
	}
	if contract.adapter == "horde" {
		cfg.Timeout = time.Second
		cfg.Options = map[string]any{"poll_interval": time.Millisecond}
	}

	backends, err := AppendFromConfig(nil, cfg)
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("len = %d, want 1", len(backends))
	}
	switch b := backends[0].(type) {
	case *OpenAI:
		b.client = srv.Client()
	case *Anthropic:
		b.client = srv.Client()
	case *Ollama:
		b.client = srv.Client()
	case *LlamaCpp:
		b.client = srv.Client()
	case *Horde:
		b.client = srv.Client()
	}
	return backends[0]
}

func writeAdapterContractGenerateResponse(t *testing.T, w http.ResponseWriter, r *http.Request, adapter string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")

	switch adapter {
	case "openai", "llamacpp":
		writeOpenAIJSON(t, w, openAIResponseBody("ok", "contract-model"))
	case "anthropic":
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: stringPtr("ok")}},
			Model:   "contract-model",
		})
	case "ollama":
		_ = json.NewEncoder(w).Encode(ollamaResponse{
			Message: spec.PromptMessage{Content: "ok"},
			Model:   "contract-model",
		})
	case "horde":
		if r.Method == http.MethodPost && r.URL.Path == "/v2/generate/text/async" {
			writeJSON(t, w, hordeAsyncResponse{ID: "contract-req"})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/generate/text/status/") {
			writeJSON(t, w, hordeStatusResponse{
				Done: true, IsPossible: true,
				Generations: []hordeGeneration{{Text: "ok", Model: "contract-model"}},
			})
			return
		}
		t.Fatalf("unexpected horde request: %s %s", r.Method, r.URL.Path)
	default:
		t.Fatalf("unsupported adapter %q", adapter)
	}
}

func assertAdapterContractToolFormat(t *testing.T, adapter string, body map[string]any) {
	t.Helper()

	switch adapter {
	case "openai":
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatal("expected tools array in OpenAI request body")
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tools[0] type = %T", tools[0])
		}
		if tool["type"] != "function" {
			t.Fatalf("tool type = %v, want function", tool["type"])
		}
	case "anthropic":
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatal("expected tools array in Anthropic request body")
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tools[0] type = %T", tools[0])
		}
		if tool["name"] != "contract_tool" {
			t.Fatalf("tool name = %v, want contract_tool", tool["name"])
		}
	default:
		t.Fatalf("tool format contract not defined for adapter %q", adapter)
	}
}

func testOpenAIContractLegacyGeminiAlias(t *testing.T) {
	t.Helper()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{Adapter: "gemini", BaseURL: "http://example"})
	if err != nil {
		t.Fatalf("AppendFromConfig(gemini) error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name() != spec.ProviderGemini {
		t.Fatalf("Name() = %q, want %q", got[0].Name(), spec.ProviderGemini)
	}
	openAI, ok := got[0].(*OpenAI)
	if !ok {
		t.Fatalf("backend type = %T, want *OpenAI", got[0])
	}
	if openAI.cfg.Adapter != string(spec.ProviderOpenAI) {
		t.Fatalf("cfg.Adapter = %q, want openai", openAI.cfg.Adapter)
	}
	if !openAI.Capabilities().SupportsChatTools {
		t.Fatal("gemini alias must inherit openai SupportsChatTools=true")
	}
}
