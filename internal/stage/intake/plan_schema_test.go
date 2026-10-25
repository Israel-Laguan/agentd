package intake

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/stage"
)

func TestPlan_WrongShapeThenCorrectionCarriesSchema(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{
		`{"project":"machine-inspection","report":{"os":"x"}}`,
		`{"project_name":"machine-inspection","tasks":[{"title":"Inspect host","description":"run uname","assignee":"SYSTEM"}]}`,
	}}
	plan, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 0, "inspect this machine")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ProjectName != "machine-inspection" || len(plan.Tasks) != 1 {
		t.Fatalf("plan = %+v, want second-attempt plan", plan)
	}
	if len(gw.seen) != 2 {
		t.Fatalf("gateway calls = %d, want 2", len(gw.seen))
	}
	msgs := gw.seen[1].Messages
	last := msgs[len(msgs)-1].Content
	for _, want := range []string{"project name is required", "project_name", "tasks", "title", "assignee"} {
		if !strings.Contains(last, want) {
			t.Fatalf("correction message missing %q:\n%s", want, last)
		}
	}
}

func TestPlan_WrongShapeIsRetriedWithSchema(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{
		`{"project":"x","plan":[]}`,
		`{"project_name":"host-report","tasks":[{"title":"t","description":"d","assignee":"SYSTEM"}]}`,
	}}
	draft, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 0, "inspect")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if draft.ProjectName != "host-report" || len(gw.seen) != 2 {
		t.Fatalf("draft = %+v, calls = %d", draft, len(gw.seen))
	}
	if sys := gw.seen[0].Messages[0].Content; !strings.Contains(sys, "project_name") {
		t.Fatalf("system prompt lacks schema: %s", sys)
	}
}

func TestPlan_CapsPhaseAndSendsItsPurpose(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{
		`{"project_name":"p","tasks":[{"title":"a","description":"d","assignee":"SYSTEM"},{"title":"b","description":"d","assignee":"SYSTEM"},{"title":"c","description":"d","assignee":"SYSTEM"}]}`,
	}}
	plan, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 2, "big")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Tasks) != 2 || plan.Tasks[1].Title != "Plan Phase 2" {
		t.Fatalf("tasks = %+v, want two tasks ending in Plan Phase 2", plan.Tasks)
	}
	if got := gw.seen[0].Purpose; got != spec.PurposeIntakeDraftPlan {
		t.Fatalf("purpose = %q, want %q", got, spec.PurposeIntakeDraftPlan)
	}
	if got := gw.seen[0].Temperature; got != 0.2 {
		t.Fatalf("temperature = %v, want 0.2", got)
	}
}
