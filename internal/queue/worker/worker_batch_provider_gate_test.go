package worker

import (
	"context"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/queue/safety"
)

// B-013 follow-up: the batched LLM call is the provider request for every task in
// the batch, so it is what the provider breaker gate has to cover.
//
// processRunningTask gates only the per-slot fallback that runs afterwards, so
// without a gate on the batch itself an OPEN breaker would still send the
// aggregated request to a provider the daemon believes is down.
func TestProcessBatch_OpenProviderBreakerBlocksTheBatchedCall(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "gemini", Model: "gpt-4", AgenticMode: false,
		},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &batchTrackingGateway{}
	sb := &batchCountingSandbox{}
	pb := safety.NewProviderBreakers()
	for range 3 {
		pb.Get("gemini").RecordError(models.ErrLLMQuotaExceeded)
	}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		Batching:         config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ProviderBreakers: pb,
	})

	w.ProcessBatch(context.Background(), tasks)

	gw.mu.Lock()
	generateCount := gw.generateCount
	gw.mu.Unlock()
	if generateCount != 0 {
		t.Fatalf("generateCount = %d, want 0 while the provider breaker is OPEN", generateCount)
	}
	sb.mu.Lock()
	calls := sb.calls
	sb.mu.Unlock()
	if calls != 0 {
		t.Fatalf("sandbox calls = %d, want 0 — no command can come from a request that was never sent", calls)
	}
	if got := pb.Get("gemini").State(); got != safety.BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN — refusing the batch must not resolve it", got)
	}
}
