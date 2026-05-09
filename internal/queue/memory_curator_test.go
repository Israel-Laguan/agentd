//go:build integration

package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/memory"
	"agentd/internal/models"
)

type curatorTestGateway struct {
	extractJSON string
}

func (g *curatorTestGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if req.JSONMode {
		return gateway.AIResponse{Content: g.extractJSON}, nil
	}
	return gateway.AIResponse{Content: "summary"}, nil
}

func (g *curatorTestGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *curatorTestGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *curatorTestGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

type closedBreaker struct{}

func (b *closedBreaker) IsOpen() bool { return false }

func TestCurateMemories_CuratesEligibleTask(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()

	project, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "test-proj",
		Tasks: []models.DraftTask{
			{Title: "do work", Description: "desc"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	tasks, _ := store.ListTasksByProject(ctx, project.ID)
	task := tasks[0]

	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim: %v", err)
	}
	task = claimed[0]
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 12345)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	completed, err := store.UpdateTaskState(ctx, running.ID, running.UpdatedAt, models.TaskStateCompleted)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	_ = completed

	_ = store.AppendEvent(ctx, models.Event{
		ProjectID: project.ID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      "LOG_CHUNK",
		Payload:   "hello world",
	})

	summaryJSON, _ := json.Marshal(map[string]string{
		"symptom":  "did work",
		"solution": "success",
	})
	sink := &recordingSink{}
	lib := &memory.Librarian{
		Store:   store,
		Gateway: &curatorTestGateway{extractJSON: string(summaryJSON)},
		Breaker: &closedBreaker{},
		Sink:    sink,
		Cfg: config.LibrarianConfig{
			RetentionHours:        0,
			ArchiveGraceDays:      7,
			ChunkChars:            50000,
			MaxReducePasses:       3,
			FallbackHeadTailChars: 2000,
		},
		HomeDir: t.TempDir(),
	}

	daemon := NewDaemon(store, nil, nil, nil, sink, DaemonOptions{
		MaxWorkers: 1,
		Librarian:  lib,
	})

	if err := daemon.curateMemories(ctx); err != nil {
		t.Fatalf("curateMemories: %v", err)
	}

	memories, err := store.ListMemories(ctx, models.MemoryFilter{Scope: "TASK_CURATION"})
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if !memories[0].Symptom.Valid || memories[0].Symptom.String != "did work" {
		t.Errorf("symptom = %v", memories[0].Symptom)
	}
}

func TestCurateMemories_NilLibrarian(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{MaxWorkers: 1})
	if err := daemon.curateMemories(context.Background()); err != nil {
		t.Fatalf("curateMemories with nil librarian: %v", err)
	}
}

func TestCuratorDelayUsesEveryWhenConfigured(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		MaxWorkers:   1,
		CuratorEvery: 42 * time.Millisecond,
	})
	if got := daemon.nextCuratorDelay(time.Now()); got != 42*time.Millisecond {
		t.Fatalf("nextCuratorDelay() = %s, want 42ms", got)
	}
}
