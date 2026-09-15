package services_test

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

type fakeProviderRegistry struct {
	names  []string
	models map[string][]string
}

func (f fakeProviderRegistry) ProviderNames() []string { return f.names }

func (f fakeProviderRegistry) KnownModels(provider string) []string {
	return f.models[provider]
}

func TestAgentServiceCreateUnknownModelWarns(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = fakeProviderRegistry{
		names:  []string{"openai"},
		models: map[string][]string{"openai": {"gpt-4o-mini"}},
	}

	result, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "m1", Name: "Agent", Provider: "openai", Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one model hint", result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "gpt-4o") || !strings.Contains(result.Warnings[0], "gpt-4o-mini") {
		t.Fatalf("warning = %q", result.Warnings[0])
	}
}

func TestAgentServiceCreateConfiguredModelNoWarning(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = fakeProviderRegistry{
		names:  []string{"openai"},
		models: map[string][]string{"openai": {"gpt-4o-mini"}},
	}

	result, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "m2", Name: "Agent", Provider: "openai", Model: "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}
}

func TestAgentServiceCreateNoModelCatalogSkipsWarning(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"openai"}}

	result, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "m3", Name: "Agent", Provider: "openai", Model: "anything",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none without catalog", result.Warnings)
	}
}

func TestAgentServicePatchUnknownModelWarns(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = fakeProviderRegistry{
		names:  []string{"gemini"},
		models: map[string][]string{"gemini": {"gemini-2.5-flash"}},
	}
	provider := "gemini"
	model := "unknown-model"

	result, err := svc.Patch(context.Background(), "default", services.AgentPatch{
		Provider: &provider,
		Model:    &model,
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one", result.Warnings)
	}
}
