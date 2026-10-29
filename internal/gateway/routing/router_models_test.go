package routing

import (
	"testing"

	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
)

func TestRouterKnownModels(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Name: "openai", Adapter: "openai", Model: "gpt-4o-mini"},
		{Name: "gemini", Adapter: "openai", Model: "gemini-2.5-flash"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	got := router.KnownModels("openai")
	if len(got) != 1 || got[0] != "gpt-4o-mini" {
		t.Fatalf("KnownModels(openai) = %v, want [gpt-4o-mini]", got)
	}
	if models := router.KnownModels("missing"); len(models) != 0 {
		t.Fatalf("KnownModels(missing) = %v, want nil/empty", models)
	}
}

func TestRouterKnownModelsIncludeRoutedAgents(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Name: "openai", Adapter: "openai", Model: "gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	router = router.WithPurposeRouting(route.Table{
		Agents: map[string]route.Agent{
			"worker": {Provider: "openai", Model: "gpt-4.1", Level: spec.LevelL1},
		},
	})
	got := router.KnownModels("openai")
	if len(got) != 2 {
		t.Fatalf("KnownModels(openai) = %v, want two configured models", got)
	}
}
