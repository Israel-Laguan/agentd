package gateway

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway/providers"
)

func (s *gatewayScenario) setRoleRoute(_ context.Context, role, prov, model string) error {
	if s.roleRoutes == nil {
		s.roleRoutes = make(map[Role]RoleTarget)
	}
	s.roleRoutes[Role(strings.ToLower(role))] = RoleTarget{Provider: prov, Model: model}
	return nil
}

func (s *gatewayScenario) threeProvidersConfigured(_ context.Context, a, b, c string) error {
	for _, n := range []string{a, b, c} {
		s.providers = append(s.providers, &fakeProvider{
			providerName: strings.ToLower(n),
			resp:         AIResponse{Content: n, ProviderUsed: strings.ToLower(n)},
		})
	}
	return nil
}

func (s *gatewayScenario) twoProvidersConfigured(_ context.Context, a, b string) error {
	for _, n := range []string{a, b} {
		s.providers = append(s.providers, &fakeProvider{
			providerName: strings.ToLower(n),
			resp:         AIResponse{Content: n, ProviderUsed: strings.ToLower(n)},
		})
	}
	return nil
}

func (s *gatewayScenario) sendRequestWithRole(_ context.Context, role string) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...).WithRoleRouting(s.roleRoutes)
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "role test"}},
		Role:     Role(strings.ToLower(role)),
	})
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	return nil
}

func (s *gatewayScenario) sendRequestWithRoleAndProvider(_ context.Context, role, prov string) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...).WithRoleRouting(s.roleRoutes)
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "override test"}},
		Role:     Role(strings.ToLower(role)),
		Provider: strings.ToLower(prov),
	})
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	return nil
}
