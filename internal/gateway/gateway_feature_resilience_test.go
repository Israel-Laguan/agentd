package gateway

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway/providers"
)

func (s *gatewayScenario) primaryProviderInvalid(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		err:          fmt.Errorf("provider rejected request: status 401"),
	})
	return nil
}

func (s *gatewayScenario) secondaryProviderOK(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		resp:         AIResponse{Content: "local", ProviderUsed: strings.ToLower(name)},
	})
	return nil
}

func (s *gatewayScenario) tertiaryProviderOK(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		resp:         AIResponse{Content: "crowd", ProviderUsed: strings.ToLower(name)},
	})
	return nil
}

func (s *gatewayScenario) providerFails(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		err:          fmt.Errorf("%s down", strings.ToLower(name)),
	})
	return nil
}

func (s *gatewayScenario) generateRequest(context.Context) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...)
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "generate"}},
	})
	return nil
}

func (s *gatewayScenario) providerAttempted(_ context.Context, name string) error {
	target := strings.ToLower(name)
	for _, p := range s.providers {
		if p.providerName == target && p.calls > 0 {
			return nil
		}
	}
	return fmt.Errorf("provider %q was not attempted", name)
}

func (s *gatewayScenario) providerUsed(_ context.Context, want string) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	if s.aiResp.ProviderUsed != strings.ToLower(want) {
		return fmt.Errorf("ProviderUsed = %q, want %q", s.aiResp.ProviderUsed, want)
	}
	return nil
}

func (s *gatewayScenario) allProvidersAttempted(context.Context) error {
	for _, p := range s.providers {
		if p.calls == 0 {
			return fmt.Errorf("provider %q was not attempted", p.providerName)
		}
	}
	return nil
}

func (s *gatewayScenario) errorMentionsAllProviders(context.Context) error {
	if s.aiErr == nil {
		return fmt.Errorf("Generate() error = nil, want error")
	}
	for _, p := range s.providers {
		if !strings.Contains(s.aiErr.Error(), p.providerName) {
			return fmt.Errorf("error %q missing provider %q", s.aiErr.Error(), p.providerName)
		}
	}
	return nil
}
