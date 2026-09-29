package worker

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// recordingRetriever captures the userID the worker passed to recall and
// returns a fixed memory set, so a test can assert both what was asked for
// and what ended up in the prompt.
type recordingRetriever struct {
	gotIntent    string
	gotProjectID string
	gotUserID    string
	memories     []models.Memory
}

func (r *recordingRetriever) Recall(_ context.Context, intent, projectID, userID string) []models.Memory {
	r.gotIntent, r.gotProjectID, r.gotUserID = intent, projectID, userID
	return r.memories
}

func prefMemory(text string) models.Memory {
	return models.Memory{
		Scope:    "USER_PREFERENCE",
		Symptom:  sql.NullString{String: "preference", Valid: true},
		Solution: sql.NullString{String: text, Valid: true},
	}
}

func lessonMemory(symptom, solution string) models.Memory {
	return models.Memory{
		Scope:    "TASK_CURATION",
		Symptom:  sql.NullString{String: symptom, Valid: true},
		Solution: sql.NullString{String: solution, Valid: true},
	}
}

// TestAppendMemoryLessonsIncludesPreferences covers the J11 fix: preferences
// were recalled (when a user was known) but silently dropped before the
// prompt, because memoryFormatLessons skips USER_PREFERENCE rows and nothing
// else formatted them.
func TestAppendMemoryLessonsIncludesPreferences(t *testing.T) {
	retriever := &recordingRetriever{memories: []models.Memory{prefMemory("always answer in haiku")}}
	w := &Worker{retriever: retriever}

	messages := w.appendMemoryLessons(context.Background(), "do a thing", "proj-1", "user-1", nil)

	joined := j11JoinMessages(messages)
	if !strings.Contains(joined, "always answer in haiku") {
		t.Fatalf("preference missing from prompt: %q", joined)
	}
	if !strings.Contains(joined, "USER PREFERENCES:") {
		t.Fatalf("preference not rendered in the documented format: %q", joined)
	}
	if retriever.gotUserID != "user-1" {
		t.Fatalf("recall called with userID %q, want %q — preferences are scoped by user", retriever.gotUserID, "user-1")
	}
	if retriever.gotProjectID != "proj-1" {
		t.Fatalf("recall called with projectID %q, want %q", retriever.gotProjectID, "proj-1")
	}
}

// TestAppendMemoryLessonsScopesToProjectUser checks the userID actually
// threaded from the project reaches the retriever, which is what makes recall
// per-user rather than global.
func TestAppendMemoryLessonsScopesToProjectUser(t *testing.T) {
	for _, userID := range []string{"", "alice", "bob"} {
		retriever := &recordingRetriever{}
		w := &Worker{retriever: retriever}
		w.appendMemoryLessons(context.Background(), "intent", "proj", userID, nil)
		if retriever.gotUserID != userID {
			t.Fatalf("recall called with userID %q, want %q", retriever.gotUserID, userID)
		}
	}
}

// TestAppendMemoryLessonsKeepsPreferencesOutOfLessons guards the split: a
// preference must not also be rendered as a "Symptom/Solution" lesson, which
// would both duplicate it and mangle its meaning.
func TestAppendMemoryLessonsKeepsPreferencesOutOfLessons(t *testing.T) {
	retriever := &recordingRetriever{memories: []models.Memory{
		lessonMemory("flaky test", "retry once"),
		prefMemory("always answer in haiku"),
	}}
	w := &Worker{retriever: retriever}

	messages := w.appendMemoryLessons(context.Background(), "intent", "proj", "user-1", nil)
	joined := j11JoinMessages(messages)

	if !strings.Contains(joined, "LESSONS LEARNED") || !strings.Contains(joined, "flaky test") {
		t.Fatalf("lesson missing from prompt: %q", joined)
	}
	lessonsPart := joined[:strings.Index(joined, "USER PREFERENCES:")]
	if strings.Contains(lessonsPart, "always answer in haiku") {
		t.Fatalf("preference leaked into the lessons block: %q", lessonsPart)
	}
}

// TestAppendMemoryLessonsNoUserYieldsNoPromptMessage documents that a project
// with no user recalls nothing user-scoped, so prompts for anonymous work are
// unchanged (important for prompt-cache stability).
func TestAppendMemoryLessonsNoUserYieldsNoPromptMessage(t *testing.T) {
	retriever := &recordingRetriever{}
	w := &Worker{retriever: retriever}

	messages := w.appendMemoryLessons(context.Background(), "intent", "proj", "", nil)
	if len(messages) != 0 {
		t.Fatalf("messages = %d, want 0 when recall returns nothing", len(messages))
	}
}

func j11JoinMessages(messages []gateway.PromptMessage) string {
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
