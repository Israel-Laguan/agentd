package routing

import (
	"testing"

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

func TestRouterKnownModelsRoleRoutes(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Name: "openai", Adapter: "openai", Model: "gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	router = router.WithRoleRouting(map[spec.Role]spec.RoleTarget{
		spec.RoleWorker: {Provider: "openai", Model: "gpt-4.1"},
	})
	got := router.KnownModels("openai")
	if len(got) != 2 {
		t.Fatalf("KnownModels(openai) = %v, want two configured models", got)
	}
}
