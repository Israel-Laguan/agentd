package services_test

import (
	"context"
	"errors"
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
	if err == nil || err.Error() != "name, provider, and model are required" {
		t.Fatalf("Create missing name: %v", err)
	}

	_, err = svc.Create(context.Background(), models.AgentProfile{Name: "n", Provider: "p", Model: "m", MaxTokens: -1})
	if err == nil || err.Error() != "max_tokens must be >= 0" {
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
	out, err := svc.Create(context.Background(), p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Role != "CODE_GEN" {
		t.Fatalf("Role = %q, want CODE_GEN", out.Role)
	}
	if len(bus.updated) != 1 || bus.updated[0].ID != out.ID {
		t.Fatalf("bus updated = %#v, want one publish for %q", bus.updated, out.ID)
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
