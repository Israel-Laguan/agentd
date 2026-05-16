package worker

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestRequestClarificationFromAgent_RejectsEmptyQuestion(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	w := &Worker{store: store}

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: time.Now()},
		ProjectID:  "proj-1",
	}
	err := w.RequestClarificationFromAgent(context.Background(), task, "   ", nil, "")
	if err == nil {
		t.Fatal("expected error for empty question")
	}
}
