package services

import (
	"context"
	"errors"
	"strings"

	"agentd/internal/models"
)

// AgentService coordinates HTTP-driven CRUD on AgentProfile records.
// Validation that requires DB transactions (delete protections, in-use
// checks) lives inside the store; this service is intentionally thin and
// only enforces request-shape rules.
type AgentService struct {
	Store models.KanbanStore
	Bus   AgentBus
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
func (s *AgentService) Create(ctx context.Context, p models.AgentProfile) (*models.AgentProfile, error) {
	if err := validateForCreate(&p); err != nil {
		return nil, err
	}
	if existing, _ := s.Store.GetAgentProfile(ctx, p.ID); existing != nil {
		return nil, models.ErrAgentProfileInUse
	}
	if err := s.Store.UpsertAgentProfile(ctx, p); err != nil {
		return nil, err
	}
	created, err := s.Store.GetAgentProfile(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	s.publishUpdated(ctx, *created)
	return created, nil
}

// Patch applies a sparse update and returns the new profile.
func (s *AgentService) Patch(ctx context.Context, id string, patch AgentPatch) (*models.AgentProfile, error) {
	current, err := s.Store.GetAgentProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	applyPatch(current, patch)
	if err := s.Store.UpsertAgentProfile(ctx, *current); err != nil {
		return nil, err
	}
	updated, err := s.Store.GetAgentProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	s.publishUpdated(ctx, *updated)
	return updated, nil
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
	if p.Name == "" || p.Provider == "" || p.Model == "" {
		return errors.New("name, provider, and model are required")
	}
	if p.MaxTokens < 0 {
		return errors.New("max_tokens must be >= 0")
	}
	if p.Role == "" {
		p.Role = "CODE_GEN"
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
}
