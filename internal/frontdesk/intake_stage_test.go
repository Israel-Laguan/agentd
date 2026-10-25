package frontdesk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

// stageAgent answers each stage call from a canned output keyed by purpose and
// records every request with the task ID it saw in ctx.
type stageAgent struct {
	outputs map[gateway.Purpose]string
	calls   []stage.StageRequest
	taskIDs []string
}

func (a *stageAgent) Call(ctx context.Context, req stage.StageRequest) (stage.StageResult, error) {
	a.calls = append(a.calls, req)
	a.taskIDs = append(a.taskIDs, gateway.TaskIDFromContext(ctx))
	return stage.StageResult{Outcome: stage.OutcomeCompleted, Output: json.RawMessage(a.outputs[req.Purpose])}, nil
}

const (
	classifyOutput = `{"intent":"plan_request","reason":"build"}`
	scopeOutput    = `{"single_scope":true,"confidence":1,"scopes":[{"id":"a","label":"A"}],"reason":"one"}`
	threeTasks     = `{"project_name":"p","tasks":[` +
		`{"title":"a","description":"d","assignee":"SYSTEM"},` +
		`{"title":"b","description":"d","assignee":"SYSTEM"},` +
		`{"title":"c","description":"d","assignee":"SYSTEM"}]}`
)

func newStageAgent() *stageAgent {
	return &stageAgent{outputs: map[gateway.Purpose]string{
		gateway.PurposeIntakeClassifyIntent: classifyOutput,
		gateway.PurposeIntakeAnalyzeScope:   scopeOutput,
		gateway.PurposeIntakeDraftPlan:      threeTasks,
	}}
}

func TestPlannerRunsIntakeStagesThroughAgent(t *testing.T) {
	t.Parallel()
	agent := newStageAgent()
	p := &Planner{Agent: agent}
	if _, err := p.PlanContent(context.Background(), nil, "build an api", nil); err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	want := []gateway.Purpose{
		gateway.PurposeIntakeClassifyIntent,
		gateway.PurposeIntakeAnalyzeScope,
		gateway.PurposeIntakeDraftPlan,
	}
	if len(agent.calls) != len(want) {
		t.Fatalf("agent calls = %d, want %d", len(agent.calls), len(want))
	}
	for i, req := range agent.calls {
		if req.Stage != stage.StageIntake {
			t.Errorf("call %d stage = %q, want %q", i, req.Stage, stage.StageIntake)
		}
		if req.Purpose != want[i] {
			t.Errorf("call %d purpose = %q, want %q", i, req.Purpose, want[i])
		}
	}
}

func TestPlannerCapsPlanToMaxTasksPerPhase(t *testing.T) {
	t.Parallel()
	p := &Planner{Agent: newStageAgent(), MaxTasksPerPhase: 2}
	raw, err := p.PlanContent(context.Background(), nil, "big build", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var plan models.DraftPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 || plan.Tasks[1].Title != "Plan Phase 2" {
		t.Fatalf("tasks = %+v, want two tasks ending in Plan Phase 2", plan.Tasks)
	}
}

func TestIntakeProcessorPlansThroughAgentWithReplanPurpose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	agent := &stageAgent{outputs: map[gateway.Purpose]string{
		gateway.PurposeCreateReplanComment: `{"project_name":"p","tasks":[{"title":"follow","description":"d","assignee":"SYSTEM"}]}`,
	}}
	if _, err := store.UpdateTaskState(ctx, tasks[0].ID, tasks[0].UpdatedAt, models.TaskStateInConsideration); err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	p := NewIntakeProcessor(store, agent, nil, nil, 0)
	ref := models.CommentRef{TaskID: tasks[0].ID, CommentEventID: "e1", Body: "please add tests"}
	if err := p.Process(ctx, ref); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(agent.calls))
	}
	req := agent.calls[0]
	if req.Stage != stage.StageIntake || req.Purpose != gateway.PurposeCreateReplanComment {
		t.Fatalf("call = stage %q purpose %q, want intake and replan comment", req.Stage, req.Purpose)
	}
	if agent.taskIDs[0] != ref.TaskID {
		t.Fatalf("task id in ctx = %q, want %q", agent.taskIDs[0], ref.TaskID)
	}
	if !strings.Contains(req.Context[1].Content, "please add tests") {
		t.Fatalf("user message = %q, want the comment text", req.Context[1].Content)
	}
}
