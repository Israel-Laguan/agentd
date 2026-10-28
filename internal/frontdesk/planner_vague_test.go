package frontdesk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/stage/intake"
	"agentd/internal/testutil"
)

// B-085: "make my app better" was refused two ways ("I'm not sure what you
// need", "cannot be turned into a concrete software project") and neither
// asked anything specific. Both refusal paths must end with the same question,
// and the question must be the last thing the user reads.
func TestPlanner_VagueRequestRefusalsAskTheSameQuestion(t *testing.T) {
	messages := make([]string, 0, 2)
	for _, intent := range []string{"out_of_scope", "unclear"} {
		gw := &mockGateway{intent: &intake.IntentAnalysis{Intent: intent, Reason: "too vague"}}
		p := newTestPlanner(t, testutil.NewFakeStore(), gw)
		out, err := p.PlanContent(context.Background(), nil, "make my app better", nil)
		if err != nil {
			t.Fatalf("PlanContent(%s) error = %v", intent, err)
		}
		var reply struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(out, &reply); err != nil {
			t.Fatalf("unmarshal %s reply: %v", intent, err)
		}
		messages = append(messages, reply.Message)
	}
	question := commonSuffix(messages[0], messages[1])
	if !strings.HasSuffix(question, "?") || len(question) < 40 {
		t.Fatalf("refusals share %q; want both to end with the same specific question:\n%s\n%s", question, messages[0], messages[1])
	}
}

func commonSuffix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return a[len(a)-i:]
}

// B-085 plan half: when the planner answers a vague request with a question
// instead of tasks, the user gets that question back, not a plan to approve.
func TestPlanner_PlanQuestionIsReturnedAsAClarification(t *testing.T) {
	const question = "Which part of the app should get better, and how will you know it did?"
	gw := &mockGateway{
		intent: &intake.IntentAnalysis{Intent: "plan_request"},
		scope:  &intake.ScopeAnalysis{SingleScope: true},
		plan:   &models.DraftPlan{ClarificationQuestion: question},
	}
	p := newTestPlanner(t, testutil.NewFakeStore(), gw)
	out, err := p.PlanContent(context.Background(), nil, "make my app better", nil)
	if err != nil {
		t.Fatalf("PlanContent error = %v", err)
	}
	var reply struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.Kind != "intent_clarification" || reply.Message != question {
		t.Fatalf("reply = %+v, want an intent_clarification carrying %q", reply, question)
	}
}

// B-085, replan path: a vague human comment on a task gets the plan step's
// question back as a comment. No follow-up tasks are created, the comment is
// processed so intake does not loop on it, and the task waits in
// IN_CONSIDERATION for the human's answer.
func TestIntakeProcessor_PlanQuestionIsPostedAndTaskWaits(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d", Tasks: []models.DraftTask{{Title: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	if err := store.AddComment(ctx, models.Comment{
		BaseEntity: models.BaseEntity{ID: "c1"}, TaskID: task.ID,
		Author: models.CommentAuthorUser, Body: "make it better",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	const question = "What should change, and how will you know it is done?"
	agent := &stubIntakeGateway{planOut: &models.DraftPlan{ClarificationQuestion: question}}
	p := NewIntakeProcessor(store, agent, nil, nil, 0)
	if err := p.Process(ctx, models.CommentRef{TaskID: task.ID, CommentEventID: "c1", Body: "make it better"}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	got, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateInConsideration {
		t.Fatalf("state = %s, want IN_CONSIDERATION while the question is open", got.State)
	}
	all, err := store.ListTasksByProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("ListTasksByProject: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("tasks = %d, want 1: a question creates no follow-ups", len(all))
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	last := comments[len(comments)-1]
	if last.Author != models.CommentAuthorFrontdesk || last.Body != question {
		t.Fatalf("last comment = %s %q, want the FRONTDESK question", last.Author, last.Body)
	}
	pending, err := store.ListUnprocessedHumanComments(ctx)
	if err != nil {
		t.Fatalf("ListUnprocessedHumanComments: %v", err)
	}
	for _, ref := range pending {
		if ref.CommentEventID == "c1" {
			t.Fatalf("comment c1 is still unprocessed; intake would ask again")
		}
	}
}
