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

// toolLister extends fakeProviderLister with tool support. An empty provider
// is the gateway cascade, which supports tools when any backend does.
type toolLister struct {
	fakeProviderLister
	toolCapable map[string]bool
}

func (l *toolLister) ProviderSupportsChatTools(provider string) bool {
	if provider == "" {
		for _, ok := range l.toolCapable {
			if ok {
				return true
			}
		}
		return false
	}
	return l.toolCapable[provider]
}

func newToolLister() *toolLister {
	return &toolLister{
		fakeProviderLister: fakeProviderLister{names: []string{"openai", "ollama"}},
		toolCapable:        map[string]bool{"openai": true, "ollama": false},
	}
}

func TestAgentServiceCreateRejectsAgenticProfileOnToolLessProvider(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = newToolLister()

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "local", Name: "Local", Provider: "ollama", Model: "llama3", AgenticMode: true,
	})
	if !errors.Is(err, models.ErrAgentProfileNoToolSupport) {
		t.Fatalf("Create() error = %v, want ErrAgentProfileNoToolSupport", err)
	}
	if got, _ := store.GetAgentProfile(context.Background(), "local"); got != nil {
		t.Fatal("rejected profile was stored")
	}
}

// Every profile runs the agentic loop, so the tool check ignores the flag. The
// message must not suggest agentic_mode false, which the API refuses.
func TestAgentServiceCreateRejectsToolLessProviderWhenFlagFalse(t *testing.T) {
	svc := services.NewAgentService(testutil.NewFakeStore(), nil)
	svc.Lister = newToolLister()

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "local", Name: "Local", Provider: "ollama", Model: "llama3", AgenticMode: false,
	})
	if !errors.Is(err, models.ErrAgentProfileNoToolSupport) {
		t.Fatalf("Create() error = %v, want ErrAgentProfileNoToolSupport", err)
	}
	if strings.Contains(err.Error(), "agentic_mode") {
		t.Fatalf("error %q suggests agentic_mode false, which is refused", err)
	}
}

func TestAgentServiceCreateAllowsAgenticProfileOnToolProvider(t *testing.T) {
	svc := services.NewAgentService(testutil.NewFakeStore(), nil)
	svc.Lister = newToolLister()

	if _, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "cloud", Name: "Cloud", Provider: "openai", Model: "gpt-4o", AgenticMode: true,
	}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
}

func TestAgentServiceCreateAgenticCascadeNeedsAToolBackend(t *testing.T) {
	svc := services.NewAgentService(testutil.NewFakeStore(), nil)
	lister := newToolLister()
	lister.toolCapable = map[string]bool{"ollama": false}
	svc.Lister = lister

	_, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "cascade", Name: "Cascade", AgenticMode: true,
	})
	if !errors.Is(err, models.ErrAgentProfileNoToolSupport) {
		t.Fatalf("Create() error = %v, want ErrAgentProfileNoToolSupport when no backend supports tools", err)
	}
}

func TestAgentServicePatchRejectsSwitchToToolLessProvider(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	svc.Lister = newToolLister()
	ctx := context.Background()
	if _, err := svc.Create(ctx, models.AgentProfile{
		ID: "cloud", Name: "Cloud", Provider: "openai", Model: "gpt-4o", AgenticMode: true,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	provider, model := "ollama", "llama3"
	_, err := svc.Patch(ctx, "cloud", services.AgentPatch{Provider: &provider, Model: &model})
	if !errors.Is(err, models.ErrAgentProfileNoToolSupport) {
		t.Fatalf("Patch() error = %v, want ErrAgentProfileNoToolSupport", err)
	}
	stored, err := store.GetAgentProfile(ctx, "cloud")
	if err != nil {
		t.Fatalf("get cloud: %v", err)
	}
	if stored.Provider != "openai" {
		t.Fatalf("stored provider = %q after rejected patch, want openai", stored.Provider)
	}
}
