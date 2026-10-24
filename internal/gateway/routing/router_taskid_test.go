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
