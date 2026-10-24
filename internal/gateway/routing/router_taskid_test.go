package routing

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
)

// taskSeenTruncator records the task the router put on the ctx it passes to
// the truncator.
type taskSeenTruncator struct {
	seen []string
}

func (s *taskSeenTruncator) Apply(ctx context.Context, msgs []spec.PromptMessage, _ int) ([]spec.PromptMessage, error) {
	s.seen = append(s.seen, spec.TaskIDFromContext(ctx))
	return msgs, nil
}

// T-057: truncation for a request runs under that request's task, so a
// summary the truncator makes is attributed to the same task.
func TestRouterHandsTheRequestTaskToTheTruncator(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	trunc := &taskSeenTruncator{}
	req := userReq(spec.PurposeExecTurn, "hello")
	req.TaskID = "task-7"

	if _, err := NewRouter(p).WithTruncation(trunc, 0).Generate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(trunc.seen) != 1 || trunc.seen[0] != "task-7" {
		t.Errorf("truncator saw tasks %q, want [task-7]", trunc.seen)
	}
}

// T-057: the record for each provider attempt names the request's task, which
// is what the per-task view of ai_calls groups by.
func TestCallRecordCarriesTheRequestTaskID(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	rec := &sliceRecorder{}
	req := userReq(spec.PurposeExecTurn, "hello")
	req.TaskID = "task-7"

	if _, err := NewRouter(p).WithRecorder(rec).Generate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 1 || rec.recs[0].TaskID != "task-7" {
		t.Errorf("records = %+v, want one record with TaskID task-7", rec.recs)
	}
}

// T-057: GeneratePlan takes no request, so the plan call records the task its
// caller put on ctx (intake replans, next-phase planning).
func TestGeneratePlanRecordsTheContextTaskID(t *testing.T) {
	t.Parallel()

	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: `{"project_name":"P","tasks":[{"title":"a","description":"d","assignee":"SYSTEM"}]}`}}}
	rec := &sliceRecorder{}
	ctx := spec.WithTaskID(context.Background(), "task-9")

	if _, err := NewRouter(p).WithRecorder(rec).GeneratePlan(ctx, "do it"); err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 1 || rec.recs[0].TaskID != "task-9" {
		t.Errorf("records = %+v, want one record with TaskID task-9", rec.recs)
	}
}
