package worker

import (
	"context"
	"log/slog"
	"testing"

	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/models"
)

func capabilityBatchWorker(registry *capabilities.Registry) *Worker {
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4"},
	}
	return NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		Capabilities: registry,
		CapabilityRouting: config.CapabilityRoutingConfig{
			Enabled:       true,
			MinConfidence: 0.35,
			Mappings:      map[string]string{agentruntime.IntentGenerateImage: "stability_api"},
		},
	})
}

func imageTask() models.Task {
	return models.Task{
		BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1", AgentID: "ag1",
		Title: "Generate an image", Description: "Draw a logo illustration for the product",
	}
}

// B-061: with no capability registry, a router-matched task can never be routed
// (TryExternalCapabilityRoute falls through), so it is not held back for one.
func TestWouldCapabilityRoute_WithoutRegistryFallsThrough(t *testing.T) {
	t.Parallel()
	w := capabilityBatchWorker(nil)
	if w.capabilityRouter == nil {
		t.Fatal("capability router not built from config")
	}
	if w.wouldCapabilityRoute(imageTask(), models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4"}) {
		t.Fatal("wouldCapabilityRoute = true, want false: no registry, so the task cannot be routed")
	}
}

func TestIsTaskBatchable_RouterMatchWithAdapterIsNotBatchable(t *testing.T) {
	t.Parallel()
	registry := capabilities.NewRegistry()
	registry.Register("stability_api", fakeCapabilityCallAdapter{name: "stability_api"})
	w := capabilityBatchWorker(registry)
	if w.isTaskBatchable(context.Background(), imageTask()) {
		t.Fatal("isTaskBatchable = true, want false: the mapped adapter exists, so the task is routed")
	}
}

// A registry without the mapped adapter also falls through in
// TryExternalCapabilityRoute ("adapter not registered").
func TestWouldCapabilityRoute_WithoutMappedAdapterFallsThrough(t *testing.T) {
	t.Parallel()
	w := capabilityBatchWorker(capabilities.NewRegistry())
	if w.wouldCapabilityRoute(imageTask(), models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4"}) {
		t.Fatal("wouldCapabilityRoute = true, want false: the mapped adapter is not registered, so the task cannot be routed")
	}
}

// B-061: an enabled router with no adapter registry is reported once at startup.
func TestNewWorker_WarnsWhenCapabilityRoutingHasNoAdapters(t *testing.T) {
	capture := &batchLogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const msg = "capability routing is enabled but no capability adapters are available; matching tasks use the normal path"
	capabilityBatchWorker(capabilities.NewRegistry())
	if capture.has(msg) {
		t.Fatal("warned although a capability registry is set")
	}
	capabilityBatchWorker(nil)
	if !capture.has(msg) {
		t.Fatalf("missing startup warning %q", msg)
	}
}

func (c *batchLogCapture) has(msg string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if r.Message == msg {
			return true
		}
	}
	return false
}
