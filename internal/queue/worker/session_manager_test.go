package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wsession "agentd/internal/agent/session"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type sessionEventSink struct {
	events *[]models.Event
}

func (s *sessionEventSink) Emit(_ context.Context, ev models.Event) error {
	*s.events = append(*s.events, ev)
	return nil
}

func TestSessionManager_PollNewHumanInput(t *testing.T) {
	sm := NewSessionManager("task-1", "initial", nil)
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorWorkerAgent, Body: "internal",
	})
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now.Add(time.Second)},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "steer me",
	})
	body, ok := sm.PollNewHumanInput(context.Background(), store, "task-1")
	if !ok || body != "steer me" {
		t.Fatalf("PollNewHumanInput = (%q, %v), want (steer me, true)", body, ok)
	}
	body, ok = sm.PollNewHumanInput(context.Background(), store, "task-1")
	if ok {
		t.Fatalf("expected no second poll, got %q", body)
	}
}

func TestSessionManager_ArchiveAndReset_InheritsPrefsNotHistory(t *testing.T) {
	dir := t.TempDir()
	prefsPath := filepath.Join(dir, "prefs.yaml")
	if err := os.WriteFile(prefsPath, []byte("preferences:\n  tone: concise\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var events []models.Event
	w := NewWorker(nil, nil, nil, nil, &sessionEventSink{events: &events}, WorkerOptions{
		InstructionsUserPrefsPath: prefsPath,
	})
	sm := NewSessionManager("task-1", "CSS styling help", wsession.NewMemoryCheckpointStore())

	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}, Title: "T", Description: "old task"}
	project := models.Project{}
	profile := models.AgentProfile{}

	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "CSS styling help"},
		{Role: "assistant", Content: "I updated flexbox rules"},
		{Role: "tool", Content: "ok", ToolCallID: "c1"},
		{Role: "system", Content: "PREVIOUS CONTEXT SUMMARY (Compressed):\n- Work: css"},
	}

	_, err := sm.ArchiveAndReset(context.Background(), w, task, project, profile, &messages, "database migrations")
	if err != nil {
		t.Fatalf("ArchiveAndReset: %v", err)
	}
	if sm.Generation() != 1 {
		t.Fatalf("generation = %d, want 1", sm.Generation())
	}
	if sm.TopicAnchor() != "database migrations" {
		t.Fatalf("topic anchor = %q", sm.TopicAnchor())
	}
	for _, m := range messages {
		if m.Role == "assistant" || m.Role == "tool" {
			t.Fatalf("history should be cleared, found role %s", m.Role)
		}
		if strings.Contains(m.Content, "PREVIOUS CONTEXT SUMMARY") {
			t.Fatal("compressed summary should not appear in fresh session")
		}
	}
	if len(messages) < 2 {
		t.Fatalf("expected system+user messages, got %d", len(messages))
	}
	if !strings.Contains(messages[0].Content, "User Preferences") {
		t.Fatal("fresh session should inherit user preferences in system prompt")
	}
	if messages[1].Role != "user" || messages[1].Content != "database migrations" {
		t.Fatalf("user turn = %+v, want drift input as first turn", messages[1])
	}
	if len(events) != 1 || events[0].Type != models.EventTypeTopicDrift {
		t.Fatalf("events = %#v, want TOPIC_DRIFT", events)
	}
}

func TestSessionManager_ArchiveAndReset_SkipsCodeGenTemplateOnDrift(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{promptLibrary: lib}
	sm := NewSessionManager("task-1", "Implement add", nil)

	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-1"},
		Title:       "Implement add",
		Description: "Add function in math.go\nSignature:\nfunc Add(a, b int) int\nTest cases:\n- Add(1,2) == 3",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeCodeGen}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys with raw source code"},
		{Role: "user", Content: "Implement add"},
		{Role: "assistant", Content: "done"},
	}

	_, err = sm.ArchiveAndReset(context.Background(), w, task, models.Project{}, profile, &messages, "database migrations")
	if err != nil {
		t.Fatalf("ArchiveAndReset: %v", err)
	}
	if len(messages) < 2 {
		t.Fatalf("expected system+user messages, got %d", len(messages))
	}
	if strings.Contains(messages[0].Content, "raw source code") {
		t.Fatalf("drift reset should not keep CODE_PROMPT_BUILDER system text: %q", messages[0].Content)
	}
	if !strings.Contains(messages[0].Content, "autonomous agent") {
		t.Fatal("fresh session should use default instruction hierarchy")
	}
	if messages[1].Role != "user" || messages[1].Content != "database migrations" {
		t.Fatalf("user turn = %+v, want drift input", messages[1])
	}
}

func TestSessionManager_AdvanceTopic(t *testing.T) {
	sm := NewSessionManager("t", "migrations", nil)
	sm.AdvanceTopic("now add tests for the migration")
	if sm.TopicAnchor() != "now add tests for the migration" {
		t.Fatalf("anchor = %q", sm.TopicAnchor())
	}
}

func TestExtractAnchorUserContent(t *testing.T) {
	got := extractAnchorUserContent([]gateway.PromptMessage{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "  topic seed  "},
	})
	if got != "topic seed" {
		t.Fatalf("got %q", got)
	}
}
