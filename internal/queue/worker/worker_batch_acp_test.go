package worker

import (
	"context"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/config"
	"agentd/internal/gateway/route"
	"agentd/internal/models"
)

func TestProcessBatchFallbackRoutesToACP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shrink bool
	}{
		{name: "missing batch slot"},
		{name: "one runnable task", shrink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := summarizeTasks(2)
			for i := range tasks {
				tasks[i].AgentID = route.DefaultAgentID
			}
			tasks[1].Title = fakeagent.Script(report(map[string]any{"summary": "ACP fallback completed"}))
			store := &batchTestStore{
				project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: t.TempDir()},
				profile: models.AgentProfile{ID: route.DefaultAgentID, Provider: "fake", AgenticMode: true},
				tasks:   make(map[string]models.Task), results: make(map[string]*models.TaskResult),
			}
			for _, task := range tasks {
				store.tasks[task.ID] = task
			}
			if tc.shrink {
				first := store.tasks[tasks[0].ID]
				first.State = models.TaskStateRunning
				store.tasks[first.ID] = first
			}
			omit := 1
			gw := &batchTrackingGateway{omitSlot: &omit}
			rt, _, log := acpRuntime(t)
			w := NewWorker(store, gw, &routingTestSandbox{}, nil, nil, WorkerOptions{
				Routes: acpRoutes(), ACP: rt, Batching: config.BatchingConfig{Enabled: true},
			})
			w.ProcessBatch(context.Background(), tasks)
			if starts, _ := log.runs(); len(starts) != 1 {
				t.Fatalf("ACP sessions started = %d, want 1", len(starts))
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if result := store.results[tasks[1].ID]; result == nil || !result.Success {
				t.Fatalf("ACP task result = %+v, want success", result)
			}
		})
	}
}
