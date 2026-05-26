package services

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
)

// ProviderLister is a narrow interface satisfied by the gateway Router.
// It provides the live set of configured provider names for validation.
type ProviderLister interface {
	ProviderNames() []string
}

// AgentService coordinates HTTP-driven CRUD on AgentProfile records.
// Validation that requires DB transactions (delete protections, in-use
// checks) lives inside the store; this service is intentionally thin and
// only enforces request-shape rules.
type AgentService struct {
	Store  models.KanbanStore
	Bus    AgentBus
	Lister ProviderLister // optional; nil skips provider-existence validation
}

// AgentBus is the optional bus surface used to publish agent_updated /
// agent_deleted live signals. Implementations must be non-blocking.
type AgentBus interface {
	PublishAgentUpdated(ctx context.Context, profile models.AgentProfile)
	PublishAgentDeleted(ctx context.Context, agentID string)
}

// NewAgentService wires the persistence boundary used by agent controllers.
func NewAgentService(store models.KanbanStore, bus AgentBus) *AgentService {
	return &AgentService{Store: store, Bus: bus}
}

// AgentPatch is a sparse update payload. Nil fields are left untouched on
// the existing profile.
type AgentPatch struct {
	Name         *string
	Provider     *string
	Model        *string
	Temperature  *float64
	SystemPrompt *string
	Role         *string
	MaxTokens    *int
	AgenticMode           *bool
	CapabilityRouteIntent *string
}

// List returns all known agent profiles.
func (s *AgentService) List(ctx context.Context) ([]models.AgentProfile, error) {
	return s.Store.ListAgentProfiles(ctx)
}

// Get returns a single agent profile or models.ErrAgentProfileNotFound.
func (s *AgentService) Get(ctx context.Context, id string) (*models.AgentProfile, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, models.ErrAgentProfileNotFound
	}
	return s.Store.GetAgentProfile(ctx, id)
}

// Create inserts a new profile. The caller may leave ID blank to have a
// UUID assigned. Returns ErrAgentProfileInUse when the requested ID
// already exists; the manager's loop should PATCH instead.
func (s *AgentService) Create(ctx context.Context, p models.AgentProfile) (AgentWriteResult, error) {
	if err := validateForCreate(&p); err != nil {
		return AgentWriteResult{}, err
	}
	if err := s.validateProviderExists(p.Provider); err != nil {
		return AgentWriteResult{}, err
	}
	if existing, _ := s.Store.GetAgentProfile(ctx, p.ID); existing != nil {
		return AgentWriteResult{}, models.ErrAgentProfileInUse
	}
	if err := s.Store.UpsertAgentProfile(ctx, p); err != nil {
		return AgentWriteResult{}, err
	}
	created, err := s.Store.GetAgentProfile(ctx, p.ID)
	if err != nil {
		return AgentWriteResult{}, err
	}
	s.publishUpdated(ctx, *created)
	return AgentWriteResult{Profile: created, Warnings: s.warnIfModelNotConfigured(p.Provider, p.Model)}, nil
}

// Patch applies a sparse update and returns the new profile.
func (s *AgentService) Patch(ctx context.Context, id string, patch AgentPatch) (AgentWriteResult, error) {
	current, err := s.Store.GetAgentProfile(ctx, id)
	if err != nil {
		return AgentWriteResult{}, err
	}
	applyPatch(current, patch)
	current.Name = strings.TrimSpace(current.Name)
	if current.Name == "" {
		return AgentWriteResult{}, fmt.Errorf("%w: name is required", models.ErrAgentProfileInvalid)
	}
	if err := validateProviderModelPair(current.Provider, current.Model); err != nil {
		return AgentWriteResult{}, err
	}
	if err := s.validateProviderExists(current.Provider); err != nil {
		return AgentWriteResult{}, err
	}
	if err := s.Store.UpsertAgentProfile(ctx, *current); err != nil {
		return AgentWriteResult{}, err
	}
	updated, err := s.Store.GetAgentProfile(ctx, id)
	if err != nil {
		return AgentWriteResult{}, err
	}
	s.publishUpdated(ctx, *updated)
	return AgentWriteResult{
		Profile:  updated,
		Warnings: s.warnIfModelNotConfigured(current.Provider, current.Model),
	}, nil
}

// Delete removes a profile. The store enforces protected/in-use guards.
func (s *AgentService) Delete(ctx context.Context, id string) error {
	if err := s.Store.DeleteAgentProfile(ctx, id); err != nil {
		return err
	}
	if s.Bus != nil {
		s.Bus.PublishAgentDeleted(ctx, id)
	}
	return nil
}

func (s *AgentService) publishUpdated(ctx context.Context, profile models.AgentProfile) {
	if s.Bus != nil {
		s.Bus.PublishAgentUpdated(ctx, profile)
	}
}

func validateForCreate(p *models.AgentProfile) error {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Provider = strings.TrimSpace(p.Provider)
	p.Model = strings.TrimSpace(p.Model)
	p.Role = strings.TrimSpace(p.Role)
	p.CapabilityRouteIntent = strings.TrimSpace(p.CapabilityRouteIntent)
	if p.Name == "" {
		return fmt.Errorf("%w: name is required", models.ErrAgentProfileInvalid)
	}
	if err := validateProviderModelPair(p.Provider, p.Model); err != nil {
		return err
	}
	if p.MaxTokens < 0 {
		return fmt.Errorf("%w: max_tokens must be >= 0", models.ErrAgentProfileInvalid)
	}
	if p.Role == "" {
		p.Role = "CODE_GEN"
	}
	return nil
}

func (s *AgentService) validateProviderExists(provider string) error {
	if s.Lister == nil || provider == "" {
		return nil
	}
	names := s.Lister.ProviderNames()
	for _, n := range names {
		if strings.EqualFold(n, provider) {
			return nil
		}
	}
	return &models.ProviderNotConfiguredError{Provider: provider, Available: names}
}

func validateProviderModelPair(provider, model string) error {
	if (provider == "") != (model == "") {
		return fmt.Errorf("%w: provider and model must both be set or both empty for gateway cascade", models.ErrAgentProfileInvalid)
	}
	return nil
}

func applyPatch(profile *models.AgentProfile, patch AgentPatch) {
	if patch.Name != nil {
		profile.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Provider != nil {
		profile.Provider = strings.TrimSpace(*patch.Provider)
	}
	if patch.Model != nil {
		profile.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.Temperature != nil {
		profile.Temperature = *patch.Temperature
	}
	if patch.SystemPrompt != nil {
		profile.SystemPrompt.Valid = true
		profile.SystemPrompt.String = *patch.SystemPrompt
	}
	if patch.Role != nil {
		profile.Role = strings.TrimSpace(*patch.Role)
	}
	if patch.MaxTokens != nil && *patch.MaxTokens >= 0 {
		profile.MaxTokens = *patch.MaxTokens
	}
	if patch.AgenticMode != nil {
		profile.AgenticMode = *patch.AgenticMode
	}
	if patch.CapabilityRouteIntent != nil {
		profile.CapabilityRouteIntent = strings.TrimSpace(*patch.CapabilityRouteIntent)
	}
}
