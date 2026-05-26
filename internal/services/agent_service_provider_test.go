package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

// fakeProviderLister is a test stub for services.ProviderLister.
type fakeProviderLister struct {
	names []string
}

func (f *fakeProviderLister) ProviderNames() []string { return f.names }

func TestAgentServiceCreateUnknownProvider(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"openai", "gemini"}}

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "bad1", Name: "Bad", Provider: "poolside", Model: "poolside/laguna",
	})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	var pnc *models.ProviderNotConfiguredError
	if !errors.As(err, &pnc) {
		t.Fatalf("expected ProviderNotConfiguredError, got %T: %v", err, err)
	}
	if pnc.Provider != "poolside" {
		t.Fatalf("Provider = %q, want poolside", pnc.Provider)
	}
	if !strings.Contains(err.Error(), "openai") || !strings.Contains(err.Error(), "gemini") {
		t.Fatalf("error message missing available providers: %v", err)
	}
}

func TestAgentServiceCreateKnownProviderPasses(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"openai", "gemini"}}

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "good1", Name: "Good", Provider: "openai", Model: "gpt-4",
	})
	if err != nil {
		t.Fatalf("Create known provider: %v", err)
	}
}

func TestAgentServiceCreateEmptyProviderCascadePasses(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"openai", "gemini"}}

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "cascade1", Name: "Cascade", Provider: "", Model: "",
	})
	if err != nil {
		t.Fatalf("Create empty provider (cascade mode): %v", err)
	}
}

func TestAgentServiceCreateNoListerAllowsAny(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil) // Lister is nil

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "any1", Name: "Any", Provider: "poolside", Model: "poolside/laguna",
	})
	if err != nil {
		t.Fatalf("Create without lister should allow any provider: %v", err)
	}
}

func TestAgentServicePatchUnknownProvider(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"openai", "gemini"}}

	badProvider := "unknown-corp"
	badModel := "unknown-corp/v1"
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{
		Provider: &badProvider,
		Model:    &badModel,
	})
	if err == nil {
		t.Fatal("expected error for unknown provider in patch, got nil")
	}
	var pnc *models.ProviderNotConfiguredError
	if !errors.As(err, &pnc) {
		t.Fatalf("expected ProviderNotConfiguredError, got %T: %v", err, err)
	}
}

func TestAgentServicePatchKnownProviderPasses(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"openai", "gemini"}}

	provider := "gemini"
	model := "gemini-pro"
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{
		Provider: &provider,
		Model:    &model,
	})
	if err != nil {
		t.Fatalf("Patch known provider: %v", err)
	}
}

func TestAgentServiceCreateCaseInsensitiveProvider(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = &fakeProviderLister{names: []string{"OpenAI"}}

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "ci1", Name: "CaseTest", Provider: "openai", Model: "gpt-4",
	})
	if err != nil {
		t.Fatalf("Create case-insensitive provider: %v", err)
	}
}
