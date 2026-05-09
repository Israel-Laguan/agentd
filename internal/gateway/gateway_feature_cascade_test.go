package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentd/internal/gateway/providers"
	"agentd/internal/models"
)

func (s *gatewayScenario) providerAvailable(_ context.Context, name string) error {
	n := strings.ToLower(name)
	s.providers = append(s.providers, &fakeProvider{
		providerName: n,
		resp:         AIResponse{Content: n + " ok", ProviderUsed: n},
	})
	return nil
}

func (s *gatewayScenario) routerProcessesRequest(context.Context) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...)
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "cascade test"}},
	})
	return nil
}

func (s *gatewayScenario) errorWrapsUnreachable(context.Context) error {
	if s.aiErr == nil {
		return fmt.Errorf("expected error, got nil")
	}
	if !errors.Is(s.aiErr, models.ErrLLMUnreachable) {
		return fmt.Errorf("error = %v, want ErrLLMUnreachable", s.aiErr)
	}
	return nil
}

func (s *gatewayScenario) errorMentionsFourProviders(context.Context) error {
	if s.aiErr == nil {
		return fmt.Errorf("expected error, got nil")
	}
	for _, p := range s.providers {
		if !strings.Contains(s.aiErr.Error(), p.providerName) {
			return fmt.Errorf("error %q missing provider %q", s.aiErr.Error(), p.providerName)
		}
	}
	return nil
}
