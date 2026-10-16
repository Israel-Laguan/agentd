package worker

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

// batchLogCapture records every slog record while installed as the default logger.
type batchLogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *batchLogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *batchLogCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *batchLogCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *batchLogCapture) WithGroup(string) slog.Handler      { return c }

func TestProcessBatch_LogsBatchSizeAndTaskIDs(t *testing.T) {
	capture := &batchLogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(prev) })

	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	w := NewWorker(store, &batchTrackingGateway{}, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})

	w.ProcessBatch(context.Background(), tasks)

	wantIDs := make([]string, len(tasks))
	for i, task := range tasks {
		wantIDs[i] = task.ID
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, r := range capture.records {
		if r.Message != "worker: processing batch" {
			continue
		}
		var gotSize int64
		var gotIDs []string
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "batch_size":
				gotSize = a.Value.Int64()
			case "task_ids":
				gotIDs, _ = a.Value.Any().([]string)
			}
			return true
		})
		if gotSize != int64(len(tasks)) {
			t.Fatalf("batch_size = %d, want %d", gotSize, len(tasks))
		}
		if !slices.Equal(gotIDs, wantIDs) {
			t.Fatalf("task_ids = %v, want %v", gotIDs, wantIDs)
		}
		return
	}
	t.Fatal(`no "worker: processing batch" debug record was logged`)
}
