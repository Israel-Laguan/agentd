package worker

import (
	"context"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

func TestTaskBatcher_Group_DisabledSingletons(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{
		Batching: config.BatchingConfig{Enabled: false, MaxBatchSize: 5},
	})
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "a"}, ProjectID: "p1", AgentID: "ag1"},
		{BaseEntity: models.BaseEntity{ID: "b"}, ProjectID: "p1", AgentID: "ag1"},
	}
	batches := w.GroupClaimed(context.Background(), tasks)
	if len(batches) != 2 {
		t.Fatalf("batches len = %d, want 2", len(batches))
	}
}

func TestTaskBatcher_Group_SameProjectAgent(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
	}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize A", Description: "condense doc A"},
		{BaseEntity: models.BaseEntity{ID: "t2"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize B", Description: "condense doc B"},
		{BaseEntity: models.BaseEntity{ID: "t3"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize C", Description: "condense doc C"},
	}
	batches := w.GroupClaimed(context.Background(), tasks)
	if len(batches) != 1 {
		t.Fatalf("batches len = %d, want 1", len(batches))
	}
	if len(batches[0].Tasks) != 3 {
		t.Fatalf("batch tasks = %d, want 3", len(batches[0].Tasks))
	}
}

func TestTaskBatcher_Group_CrossProjectNeverBatched(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
	}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize A", Description: "condense"},
		{BaseEntity: models.BaseEntity{ID: "t2"}, ProjectID: "p2", AgentID: "ag1", Title: "Summarize B", Description: "condense"},
	}
	batches := w.GroupClaimed(context.Background(), tasks)
	if len(batches) != 2 {
		t.Fatalf("batches len = %d, want 2", len(batches))
	}
}

func TestTaskBatcher_Group_ToolRequiredExcluded(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
	}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize notes", Description: "condense recap"},
		{BaseEntity: models.BaseEntity{ID: "t2"}, ProjectID: "p1", AgentID: "ag1", Title: "Fix login bug", Description: "implement patch and add test"},
	}
	batches := w.GroupClaimed(context.Background(), tasks)
	if len(batches) != 2 {
		t.Fatalf("batches len = %d, want 2 (tool task isolated)", len(batches))
	}
	for _, b := range batches {
		if len(b.Tasks) != 1 {
			t.Fatalf("expected singleton batch, got %d tasks", len(b.Tasks))
		}
	}
}

func TestTaskBatcher_Group_DependencyConflictSplit(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
	}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 5},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize 1", Description: "condense"},
		{BaseEntity: models.BaseEntity{ID: "t2"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize 2", Description: "condense", DependsOn: []string{"t1"}},
		{BaseEntity: models.BaseEntity{ID: "t3"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize 3", Description: "condense"},
	}
	batches := w.GroupClaimed(context.Background(), tasks)
	if len(batches) != 2 {
		t.Fatalf("batches len = %d, want 2", len(batches))
	}
	for _, b := range batches {
		ids := make(map[string]struct{}, len(b.Tasks))
		for _, task := range b.Tasks {
			ids[task.ID] = struct{}{}
		}
		if _, hasT1 := ids["t1"]; hasT1 {
			if _, hasT2 := ids["t2"]; hasT2 {
				t.Fatal("dependent tasks t1 and t2 must not share a batch")
			}
		}
	}
}

func TestTaskBatcher_Group_MaxBatchSize(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
	}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{
		Batching:     config.BatchingConfig{Enabled: true, MaxBatchSize: 2},
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := []models.Task{
		{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize 1", Description: "condense"},
		{BaseEntity: models.BaseEntity{ID: "t2"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize 2", Description: "condense"},
		{BaseEntity: models.BaseEntity{ID: "t3"}, ProjectID: "p1", AgentID: "ag1", Title: "Summarize 3", Description: "condense"},
	}
	batches := w.GroupClaimed(context.Background(), tasks)
	if len(batches) != 2 {
		t.Fatalf("batches len = %d, want 2", len(batches))
	}
	if len(batches[0].Tasks) != 2 || len(batches[1].Tasks) != 1 {
		t.Fatalf("batch sizes = [%d, %d], want [2, 1]", len(batches[0].Tasks), len(batches[1].Tasks))
	}
}
