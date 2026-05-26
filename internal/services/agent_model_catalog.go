package services

import (
	"fmt"
	"strings"

	"agentd/internal/models"
)

// ProviderModelCatalog supplies gateway-configured model hints per provider name.
// Implemented by the gateway Router; optional on AgentService.Lister.
type ProviderModelCatalog interface {
	KnownModels(provider string) []string
}

// AgentWriteResult is returned from create/patch when the profile was persisted.
type AgentWriteResult struct {
	Profile  *models.AgentProfile
	Warnings []string
}

func (s *AgentService) warnIfModelNotConfigured(provider, model string) []string {
	if s.Lister == nil || provider == "" || model == "" {
		return nil
	}
	catalog, ok := s.Lister.(ProviderModelCatalog)
	if !ok {
		return nil
	}
	known := catalog.KnownModels(provider)
	if len(known) == 0 {
		return nil
	}
	for _, k := range known {
		if strings.EqualFold(k, model) {
			return nil
		}
	}
	return []string{fmt.Sprintf(
		"model %q is not among gateway-configured models for provider %q (%s); the provider may still accept it at runtime",
		model, provider, strings.Join(known, ", "),
	)}
}
