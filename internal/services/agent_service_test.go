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

type recordingAgentBus struct {
	updated []models.AgentProfile
	deleted []string
}

func (r *recordingAgentBus) PublishAgentUpdated(_ context.Context, p models.AgentProfile) {
	r.updated = append(r.updated, p)
}

func (r *recordingAgentBus) PublishAgentDeleted(_ context.Context, id string) {
	r.deleted = append(r.deleted, id)
}

func TestAgentServiceGetEmptyID(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	_, err := svc.Get(context.Background(), "   ")
	if !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("Get empty id: %v", err)
	}
}

func TestAgentServiceListDelegates(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 1 {
		t.Fatalf("expected at least default profile, got %d", len(list))
	}
}

func TestAgentServiceCreateValidation(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	_, err := svc.Create(context.Background(), models.AgentProfile{Name: "", Provider: "p", Model: "m"})
	if !errors.Is(err, models.ErrAgentProfileInvalid) || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Create missing name: %v", err)
	}

	_, err = svc.Create(context.Background(), models.AgentProfile{Name: "Cascade", Provider: "", Model: "", Role: "CODE_GEN"})
	if err != nil {
		t.Fatalf("Create cascade mode (empty provider/model): %v", err)
	}

	_, err = svc.Create(context.Background(), models.AgentProfile{ID: "partial", Name: "Partial", Provider: "openai", Model: ""})
	if !errors.Is(err, models.ErrAgentProfileInvalid) || !strings.Contains(err.Error(), "provider and model must both be set or both empty") {
		t.Fatalf("Create partial provider/model: %v", err)
	}

	_, err = svc.Create(context.Background(), models.AgentProfile{Name: "n", Provider: "p", Model: "m", MaxTokens: -1})
	if !errors.Is(err, models.ErrAgentProfileInvalid) || !strings.Contains(err.Error(), "max_tokens must be >= 0") {
		t.Fatalf("Create bad max_tokens: %v", err)
	}
}

func TestAgentServiceCreateDuplicateID(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	p := models.AgentProfile{ID: "dup", Name: "A", Provider: "openai", Model: "gpt", Role: "CODE_GEN"}
	if _, err := svc.Create(context.Background(), p); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := svc.Create(context.Background(), p)
	if !errors.Is(err, models.ErrAgentProfileInUse) {
		t.Fatalf("second Create: %v", err)
	}
}

func TestAgentServiceCreateDefaultRoleAndBus(t *testing.T) {
	store := testutil.NewFakeStore()
	bus := &recordingAgentBus{}
	svc := services.NewAgentService(store, bus)

	p := models.AgentProfile{ID: "new1", Name: "Agent", Provider: "openai", Model: "gpt-4"}
	result, err := svc.Create(context.Background(), p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Profile.Role != "CODE_GEN" {
		t.Fatalf("Role = %q, want CODE_GEN", result.Profile.Role)
	}
	if len(bus.updated) != 1 || bus.updated[0].ID != result.Profile.ID {
		t.Fatalf("bus updated = %#v, want one publish for %q", bus.updated, result.Profile.ID)
	}
}

func TestAgentServicePatch(t *testing.T) {
	store := testutil.NewFakeStore()
	bus := &recordingAgentBus{}
	svc := services.NewAgentService(store, bus)

	name := "Renamed"
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{Name: &name})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if got.Name != "Renamed" {
		t.Fatalf("Name = %q", got.Name)
	}
	if len(bus.updated) != 1 {
		t.Fatalf("expected one bus publish, got %d", len(bus.updated))
	}
}

func TestAgentServicePatchAgenticMode(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	enable := true
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{AgenticMode: &enable})
	if err != nil {
		t.Fatalf("Patch enable: %v", err)
	}
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if !got.AgenticMode {
		t.Fatal("expected AgenticMode true")
	}

	disable := false
	_, err = svc.Patch(context.Background(), "default", services.AgentPatch{AgenticMode: &disable})
	if err != nil {
		t.Fatalf("Patch disable: %v", err)
	}
	got, err = store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile after disable: %v", err)
	}
	if got.AgenticMode {
		t.Fatal("expected AgenticMode false after disable patch")
	}
}

func TestAgentServiceCreateCapabilityRouteIntent(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	result, err := svc.Create(context.Background(), models.AgentProfile{
		ID: "intent1", Name: "Agent", Provider: "openai", Model: "gpt-4",
		CapabilityRouteIntent: "  generate_image  ",
	})
	if err != nil {
		t.Fatalf("Create with intent: %v", err)
	}
	if result.Profile.CapabilityRouteIntent != "generate_image" {
		t.Fatalf("CapabilityRouteIntent = %q, want generate_image", result.Profile.CapabilityRouteIntent)
	}

	result, err = svc.Create(context.Background(), models.AgentProfile{
		ID: "intent2", Name: "Agent2", Provider: "openai", Model: "gpt-4",
		CapabilityRouteIntent: "   ",
	})
	if err != nil {
		t.Fatalf("Create with whitespace intent: %v", err)
	}
	if result.Profile.CapabilityRouteIntent != "" {
		t.Fatalf("CapabilityRouteIntent = %q, want empty after whitespace-only", result.Profile.CapabilityRouteIntent)
	}
}

func TestAgentServicePatchCapabilityRouteIntent(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	intent := "generate_image"
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{CapabilityRouteIntent: &intent})
	if err != nil {
		t.Fatalf("Patch set intent: %v", err)
	}
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if got.CapabilityRouteIntent != "generate_image" {
		t.Fatalf("CapabilityRouteIntent = %q, want generate_image", got.CapabilityRouteIntent)
	}

	clear := ""
	_, err = svc.Patch(context.Background(), "default", services.AgentPatch{CapabilityRouteIntent: &clear})
	if err != nil {
		t.Fatalf("Patch clear intent: %v", err)
	}
	got, err = store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile after clear: %v", err)
	}
	if got.CapabilityRouteIntent != "" {
		t.Fatalf("CapabilityRouteIntent = %q, want empty after clear", got.CapabilityRouteIntent)
	}
}

func TestAgentServiceDeleteProtectedAndBus(t *testing.T) {
	store := testutil.NewFakeStore()
	bus := &recordingAgentBus{}
	svc := services.NewAgentService(store, bus)

	if err := svc.Delete(context.Background(), "default"); !errors.Is(err, models.ErrAgentProfileProtected) {
		t.Fatalf("Delete default: %v", err)
	}
	if len(bus.deleted) != 0 {
		t.Fatalf("bus should not fire on failed delete")
	}

	p := models.AgentProfile{ID: "tmp", Name: "T", Provider: "openai", Model: "m", Role: "CODE_GEN"}
	if _, err := svc.Create(context.Background(), p); err != nil {
		t.Fatalf("Create tmp: %v", err)
	}
	if err := svc.Delete(context.Background(), "tmp"); err != nil {
		t.Fatalf("Delete tmp: %v", err)
	}
	if len(bus.deleted) != 1 || bus.deleted[0] != "tmp" {
		t.Fatalf("bus deleted = %#v", bus.deleted)
	}
}

func TestAgentServiceGetNotFound(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	_, err := svc.Get(context.Background(), "missing-agent")
	if !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
}

func TestAgentServicePatchAllScalarFields(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	provider := "  anthropic  "
	model := " claude-3 "
	temp := 0.42
	prompt := "be concise"
	role := "  REVIEWER  "
	max := 4096
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{
		Provider:     &provider,
		Model:        &model,
		Temperature:  &temp,
		SystemPrompt: &prompt,
		Role:         &role,
		MaxTokens:    &max,
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if got.Provider != "anthropic" || got.Model != "claude-3" {
		t.Fatalf("provider/model = %q / %q", got.Provider, got.Model)
	}
	if got.Temperature != 0.42 {
		t.Fatalf("Temperature = %v", got.Temperature)
	}
	if !got.SystemPrompt.Valid || got.SystemPrompt.String != "be concise" {
		t.Fatalf("SystemPrompt = %#v", got.SystemPrompt)
	}
	if got.Role != "REVIEWER" || got.MaxTokens != 4096 {
		t.Fatalf("Role/MaxTokens = %q / %d", got.Role, got.MaxTokens)
	}
}

func TestAgentServicePatchMaxTokensNegativeIgnored(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	ok := 512
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{MaxTokens: &ok})
	if err != nil {
		t.Fatalf("Patch set max: %v", err)
	}

	bad := -1
	_, err = svc.Patch(context.Background(), "default", services.AgentPatch{MaxTokens: &bad})
	if err != nil {
		t.Fatalf("Patch negative max: %v", err)
	}
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if got.MaxTokens != 512 {
		t.Fatalf("MaxTokens = %d, want 512 (negative patch ignored)", got.MaxTokens)
	}
}

func TestAgentServicePatchProviderModelPair(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	provider := "openai"
	model := "gpt-4"
	if _, err := svc.Patch(context.Background(), "default", services.AgentPatch{
		Provider: &provider,
		Model:    &model,
	}); err != nil {
		t.Fatalf("Patch set provider/model: %v", err)
	}

	empty := ""
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{Provider: &empty})
	if !errors.Is(err, models.ErrAgentProfileInvalid) {
		t.Fatalf("Patch error = %v, want ErrAgentProfileInvalid", err)
	}
	if !strings.Contains(err.Error(), "provider and model must both be set or both empty") {
		t.Fatalf("Patch error = %v, want provider/model pair error", err)
	}

	if _, err := svc.Patch(context.Background(), "default", services.AgentPatch{
		Provider: &empty,
		Model:    &empty,
	}); err != nil {
		t.Fatalf("Patch clear both provider/model: %v", err)
	}

	onlyModel := "gpt-4"
	_, err = svc.Patch(context.Background(), "default", services.AgentPatch{Model: &onlyModel})
	if !errors.Is(err, models.ErrAgentProfileInvalid) {
		t.Fatalf("Patch set model only: %v, want ErrAgentProfileInvalid", err)
	}
}

func TestAgentServicePatchNotFound(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	name := "nope"
	_, err := svc.Patch(context.Background(), "ghost", services.AgentPatch{Name: &name})
	if !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("Patch missing agent: %v", err)
	}
}

type upsertFailAgentStore struct {
	*testutil.FakeKanbanStore
}

func (s *upsertFailAgentStore) UpsertAgentProfile(context.Context, models.AgentProfile) error {
	return errors.New("upsert boom")
}

func TestAgentServicePatchUpsertError(t *testing.T) {
	store := &upsertFailAgentStore{FakeKanbanStore: testutil.NewFakeStore()}
	svc := services.NewAgentService(store, nil)

	name := "fail"
	_, err := svc.Patch(context.Background(), "default", services.AgentPatch{Name: &name})
	if err == nil || err.Error() != "upsert boom" {
		t.Fatalf("Patch upsert error: %v", err)
	}
}

func TestAgentServiceDeleteInUse(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)

	p := models.AgentProfile{ID: "worker", Name: "W", Provider: "openai", Model: "m", Role: "CODE_GEN"}
	if _, err := svc.Create(context.Background(), p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	plan := models.DraftPlan{ProjectName: "P", Description: "d", Tasks: []models.DraftTask{{Title: "t"}}}
	proj, tasks, err := store.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	task := tasks[0]
	_ = proj
	_, err = store.AssignTaskAgent(context.Background(), task.ID, task.UpdatedAt, "worker")
	if err != nil {
		t.Fatalf("AssignTaskAgent: %v", err)
	}

	err = svc.Delete(context.Background(), "worker")
	if !errors.Is(err, models.ErrAgentProfileInUse) {
		t.Fatalf("Delete in-use agent: %v", err)
	}
}
