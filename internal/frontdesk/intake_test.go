package frontdesk

import (
	"context"
	"fmt"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type stubIntakeGateway struct {
	structDraft models.DraftPlan
	planOut     *models.DraftPlan
	planErr     error
}

func (g *stubIntakeGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}

func (g *stubIntakeGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return g.planOut, g.planErr
}

func (g *stubIntakeGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return &gateway.ScopeAnalysis{}, nil
}

func (g *stubIntakeGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return &gateway.IntentAnalysis{}, nil
}

func (g *stubIntakeGateway) GenerateText(context.Context, string, int) (string, error) { return "", nil }

func (g *stubIntakeGateway) GenerateStructuredJSON(_ context.Context, _ string, target interface{}) error {
	if p, ok := target.(*models.DraftPlan); ok {
		*p = g.structDraft
	}
	return nil
}

func (g *stubIntakeGateway) TruncateToBudget(input string, _ int) string { return input }

type stubTruncator struct{}

func (stubTruncator) Apply(context.Context, []gateway.PromptMessage, int) ([]gateway.PromptMessage, error) {
	return []gateway.PromptMessage{{Role: "user", Content: "condensed-prior-thread"}}, nil
}

func TestIntakeProcessorSkipsNonConsiderationTask(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "only"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	gw := &stubIntakeGateway{structDraft: models.DraftPlan{Tasks: []models.DraftTask{{Title: "x"}}}}
	p := NewIntakeProcessor(store, gw, nil, nil, 0)
	if err := p.Process(ctx, models.CommentRef{TaskID: task.ID, CommentEventID: "e1", Body: "hi"}); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestIntakeProcessorUsesContractAdapter(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks:         []models.DraftTask{{Title: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	_, err = store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateInConsideration)
	if err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	if _, err := store.GetTask(ctx, task.ID); err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	draft := models.DraftPlan{Tasks: []models.DraftTask{{Title: "follow-up"}}}
	gw := &stubIntakeGateway{structDraft: draft}
	p := NewIntakeProcessor(store, gw, nil, stubTruncator{}, 100)
	ref := models.CommentRef{TaskID: task.ID, CommentEventID: "c-last", Body: "new intent"}
	for i := 0; i < 6; i++ {
		if err := store.AddComment(ctx, models.Comment{
			BaseEntity: models.BaseEntity{ID: fmt.Sprintf("c%d", i)},
			TaskID:     task.ID,
			Author:     models.CommentAuthorUser,
			Body:       fmt.Sprintf("older-%d", i),
		}); err != nil {
			t.Fatalf("AddComment: %v", err)
		}
	}
	ref.CommentEventID = "c5"
	if err := p.Process(ctx, ref); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

type recordSink struct{ emits int }

func (r *recordSink) Emit(context.Context, models.Event) error {
	r.emits++
	return nil
}

func TestIntakeProcessorEmitWithSink(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P3", Description: "d",
		Tasks:       []models.DraftTask{{Title: "sink"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	_, err = store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateInConsideration)
	if err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	gw := &stubIntakeGateway{structDraft: models.DraftPlan{Tasks: []models.DraftTask{{Title: "e"}}}}
	sink := &recordSink{}
	p := NewIntakeProcessor(store, gw, sink, nil, 0)
	if err := p.Process(ctx, models.CommentRef{TaskID: task.ID, CommentEventID: "id", Body: "body"}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if sink.emits != 1 {
		t.Fatalf("sink emits = %d", sink.emits)
	}
}

func TestIntakeProcessorGeneratePlanFallback(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P2", Description: "d",
		Tasks:       []models.DraftTask{{Title: "t2"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	_, err = store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateInConsideration)
	if err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	// Gateway without ContractAdapter: type assertion fails, GeneratePlan path runs.
	gw := &stubPlanOnlyGW{out: &models.DraftPlan{Tasks: []models.DraftTask{{Title: "from-plan"}}}}
	p := NewIntakeProcessor(store, gw, nil, nil, 0)
	if err := p.Process(ctx, models.CommentRef{TaskID: task.ID, CommentEventID: "x", Body: "go"}); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

type stubPlanOnlyGW struct{ out *models.DraftPlan }

func (*stubPlanOnlyGW) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}
func (g *stubPlanOnlyGW) GeneratePlan(context.Context, string) (*models.DraftPlan, error) { return g.out, nil }
func (*stubPlanOnlyGW) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return &gateway.ScopeAnalysis{}, nil
}
func (*stubPlanOnlyGW) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return &gateway.IntentAnalysis{}, nil
}
