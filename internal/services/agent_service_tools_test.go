package services_test

import (
	"context"
	"slices"
	"testing"

	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func TestAgentServiceCreateCleansAllowedTools(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	result, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "scoped", Name: "Scoped", Provider: "openai", Model: "gpt-4",
		AllowedTools: []string{" read ", "", "bash"}, WorkspaceReadonly: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if want := []string{"read", "bash"}; !slices.Equal(result.Profile.AllowedTools, want) {
		t.Fatalf("AllowedTools = %v, want %v", result.Profile.AllowedTools, want)
	}
	if !result.Profile.WorkspaceReadonly {
		t.Fatal("WorkspaceReadonly = false, want true")
	}
}

func TestAgentServicePatchAllowedToolsAndWorkspaceReadonly(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	ctx := context.Background()
	if _, err := svc.Create(ctx, models.AgentProfile{
		ID: "scoped", Name: "Scoped", Provider: "openai", Model: "gpt-4",
		AllowedTools: []string{"read"},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	narrowed := []string{" bash "}
	readonly := true
	result, err := svc.Patch(ctx, "scoped", services.AgentPatch{AllowedTools: &narrowed, WorkspaceReadonly: &readonly})
	if err != nil {
		t.Fatalf("Patch() error = %v", err)
	}
	if want := []string{"bash"}; !slices.Equal(result.Profile.AllowedTools, want) {
		t.Fatalf("AllowedTools = %v, want %v", result.Profile.AllowedTools, want)
	}
	if !result.Profile.WorkspaceReadonly {
		t.Fatal("WorkspaceReadonly = false after patch, want true")
	}

	name := "Renamed"
	result, err = svc.Patch(ctx, "scoped", services.AgentPatch{Name: &name})
	if err != nil {
		t.Fatalf("Patch(name) error = %v", err)
	}
	if want := []string{"bash"}; !slices.Equal(result.Profile.AllowedTools, want) || !result.Profile.WorkspaceReadonly {
		t.Fatalf("a patch without the fields changed them: tools = %v, readonly = %t", result.Profile.AllowedTools, result.Profile.WorkspaceReadonly)
	}

	cleared := []string{}
	result, err = svc.Patch(ctx, "scoped", services.AgentPatch{AllowedTools: &cleared})
	if err != nil {
		t.Fatalf("Patch(clear) error = %v", err)
	}
	if len(result.Profile.AllowedTools) != 0 {
		t.Fatalf("AllowedTools = %v after clearing, want none", result.Profile.AllowedTools)
	}
}
