package kanban

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

// A consumer that reads a file a producer writes must wait for it even when
// the plan omits depends_on: materialization infers the edge, so the
// consumer starts PENDING while the producer is READY.
func TestMaterializePlanInfersFileDependencies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p",
		Tasks: []models.DraftTask{
			{ReferenceID: "produce", Title: "Collect data", Description: "Run sensor and save the output to data.txt"},
			{ReferenceID: "consume", Title: "Summarize data", Description: "Read data.txt and summarize it"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	if tasks[0].State != models.TaskStateReady {
		t.Fatalf("producer state = %s, want READY", tasks[0].State)
	}
	if tasks[1].State != models.TaskStatePending {
		t.Fatalf("consumer state = %s, want PENDING (inferred file edge)", tasks[1].State)
	}
}

// Structural plan errors surface as ErrInvalidDraftPlan so the API maps them
// to 400 instead of 500.
func TestMaterializePlanRejectsHostRead(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p",
		Tasks:       []models.DraftTask{{ReferenceID: "a", Title: "Inspect host", Description: "cat /etc/passwd"}},
	})
	if !errors.Is(err, models.ErrInvalidDraftPlan) {
		t.Fatalf("MaterializePlan() error = %v, want ErrInvalidDraftPlan", err)
	}
}
