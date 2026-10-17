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
// (TryExternalCapabilityRoute falls through), so it must stay batchable.
func TestIsTaskBatchable_RouterMatchWithoutRegistryStaysBatchable(t *testing.T) {
	t.Parallel()
	w := capabilityBatchWorker(nil)
	if w.capabilityRouter == nil {
		t.Fatal("capability router not built from config")
	}
	if !w.isTaskBatchable(context.Background(), imageTask()) {
		t.Fatal("isTaskBatchable = false, want true: no registry, so the task cannot be routed")
	}
}

func TestIsTaskBatchable_RouterMatchWithRegistryIsNotBatchable(t *testing.T) {
	t.Parallel()
	w := capabilityBatchWorker(capabilities.NewRegistry())
	if w.isTaskBatchable(context.Background(), imageTask()) {
		t.Fatal("isTaskBatchable = true, want false: a registry exists, so the task is routed")
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
