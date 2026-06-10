package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func assertAmbiguousElicitationBlocked(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, taskID string, gw *elicitationSequenceGateway) {
	t.Helper()

	assertElicitationParentBlocked(t, store, ctx, taskID)
	assertElicitationGatewayCalls(t, gw)
	assertElicitationClarificationChild(t, store, ctx, taskID)
	assertElicitationComments(t, store, ctx, taskID)
	assertElicitationQuestionsAndExpiry(t, store, ctx, taskID)
}

func assertElicitationParentBlocked(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, taskID string) {
	t.Helper()

	parent, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parent.State)
	}
}

func assertElicitationGatewayCalls(t *testing.T, gw *elicitationSequenceGateway) {
	t.Helper()

	if gw.agenticCalls != 0 {
		t.Fatalf("agentic gateway calls = %d, want 0 before clarification", gw.agenticCalls)
	}
	if gw.elicitationCalls != 1 {
		t.Fatalf("elicitation calls = %d, want 1", gw.elicitationCalls)
	}
	if !gw.isElicitorRequest(gw.requests[0]) {
		t.Fatalf("first request is not elicitor: role=%s", gw.requests[0].Role)
	}
}

func assertElicitationClarificationChild(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, taskID string) {
	t.Helper()

	children, err := store.ListChildTasks(ctx, taskID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 || !strings.HasPrefix(children[0].Title, models.HITLSubtaskTitleClarification) {
		t.Fatalf("children = %#v, want one clarification subtask", children)
	}
}

func assertElicitationComments(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, taskID string) {
	t.Helper()

	comments, err := store.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
	if comments[0].Author != models.CommentAuthorWorkerAgent || comments[1].Author != models.CommentAuthorWorkerAgent {
		t.Fatalf("comment authors = %s, %s; want WORKER_AGENT", comments[0].Author, comments[1].Author)
	}
	if !comments[1].CreatedAt.After(comments[0].CreatedAt) {
		t.Fatalf("comment order = %s, %s; want expiry after questions", comments[0].CreatedAt, comments[1].CreatedAt)
	}
}

func assertElicitationQuestionsAndExpiry(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, taskID string) {
	t.Helper()

	comments, err := store.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	questions, ok := latestElicitationQuestions(comments)
	if !ok || len(questions) != 3 {
		t.Fatalf("latest questions ok=%v len=%d; want ok=true len=3", ok, len(questions))
	}
	expiresAt, ok := parseHITLExpiry(comments)
	if !ok || expiresAt.IsZero() {
		t.Fatalf("expiry ok=%v expires_at=%s; want parsed expiry", ok, expiresAt)
	}
}
