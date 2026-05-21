package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

func testContextManager(t *testing.T) *ContextManager {
	t.Helper()
	return NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, "agent", "task")
}

func TestResolveEditContextManager_PerCallOverride(t *testing.T) {
	t.Parallel()
	editorCM := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, "agent-default", "task-default")
	sessionCM := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, "agent-session", "task-session")

	tests := []struct {
		name      string
		sessionCM *ContextManager
		editorCM  *ContextManager
		want      *ContextManager
		wantFresh bool
	}{
		{"session overrides editor", sessionCM, editorCM, sessionCM, false},
		{"editor when session nil", nil, editorCM, editorCM, false},
		{"default when both nil", nil, nil, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveEditContextManager(tc.sessionCM, tc.editorCM)
			if tc.wantFresh {
				if got == nil {
					t.Fatal("resolveEditContextManager() = nil, want non-nil default")
				}
				return
			}
			if got != tc.want {
				t.Fatalf("resolveEditContextManager() = %p, want %p", got, tc.want)
			}
		})
	}
}

func TestMessageEditor_Edit_PerCallCmOverride(t *testing.T) {
	t.Parallel()
	defaultCM := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, "agent-default", "task-default")
	sessionCM := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, "agent-session", "task-session")
	if resolveEditContextManager(sessionCM, defaultCM) != sessionCM {
		t.Fatal("precondition: per-call cm must take precedence over editor default")
	}
	editor := NewMessageEditor(NewMemoryCheckpointStore(), nil, defaultCM)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "user", Content: "clarify"},
		{Role: "assistant", Content: "wrong"},
		{Role: "assistant", Content: "later"},
	}
	_, err := editor.Edit(context.Background(), "sess", "sess:0", &messages, 0, "revised clarify", sessionCM)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3 (anchor + revised turn0)", len(messages))
	}
	if messages[2].Content != "revised clarify" {
		t.Fatalf("turn0 user = %q, want revised clarify", messages[2].Content)
	}
	for _, m := range messages {
		if m.Content == "later" || m.Content == "wrong" {
			t.Fatalf("downstream content should be dropped, got %+v", messages)
		}
	}
}

func TestMessageEditor_Edit_TruncatesFromTurn(t *testing.T) {
	t.Parallel()
	cm := testContextManager(t)
	editor := NewMessageEditor(NewMemoryCheckpointStore(), nil, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "user", Content: "clarify"},
		{Role: "assistant", Content: "wrong"},
		{Role: "assistant", Content: "later"},
	}
	_, err := editor.Edit(context.Background(), "sess", "sess:0", &messages, 0, "revised clarify", nil)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3 (anchor + revised turn0)", len(messages))
	}
	if messages[2].Content != "revised clarify" {
		t.Fatalf("turn0 user = %q, want revised clarify", messages[2].Content)
	}
	for _, m := range messages {
		if m.Content == "later" || m.Content == "wrong" {
			t.Fatalf("downstream content should be dropped, got %+v", messages)
		}
	}
}

func TestMessageEditor_Edit_RewritesUserContent(t *testing.T) {
	t.Parallel()
	cm := testContextManager(t)
	editor := NewMessageEditor(NewMemoryCheckpointStore(), nil, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original task"},
		{Role: "assistant", Content: "bad"},
	}
	_, err := editor.Edit(context.Background(), "sess", "sess:0", &messages, EditAnchorUserTurn, "new task spec", nil)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if messages[1].Content != "new task spec" {
		t.Fatalf("anchor user content = %q, want new task spec", messages[1].Content)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want anchor only after anchor edit", len(messages))
	}
}

func TestMessageEditor_Edit_CheckpointsBeforeMutate(t *testing.T) {
	t.Parallel()
	cm := testContextManager(t)
	store := NewMemoryCheckpointStore()
	editor := NewMessageEditor(store, nil, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "stale"},
	}
	result, err := editor.Edit(context.Background(), "sess-1", "sess-1:0", &messages, EditAnchorUserTurn, "task v2", nil)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if result.CheckpointID == "" {
		t.Fatal("expected checkpoint id")
	}
	if result.MessagesBefore != 3 || result.MessagesAfter != 2 {
		t.Fatalf("before/after = %d/%d, want 3/2", result.MessagesBefore, result.MessagesAfter)
	}
	cp, err := store.Get(context.Background(), result.CheckpointID)
	if err != nil {
		t.Fatalf("Get checkpoint: %v", err)
	}
	if len(cp.Messages) != 3 {
		t.Fatalf("checkpoint message count = %d, want 3", len(cp.Messages))
	}
	if cp.Messages[2].Content != "stale" {
		t.Fatalf("checkpoint should preserve pre-edit history, last = %q", cp.Messages[2].Content)
	}
}

func TestMessageEditor_Commit_AppendsOnly(t *testing.T) {
	t.Parallel()
	editor := NewMessageEditor(nil, nil, nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "hi"}}
	editor.Commit(&messages, gateway.PromptMessage{Role: "assistant", Content: "ok"})
	if len(messages) != 2 {
		t.Fatalf("len = %d, want 2", len(messages))
	}
	if messages[1].Role != "assistant" || messages[1].Content != "ok" {
		t.Fatalf("committed = %+v", messages[1])
	}
}

func TestMessageEditor_Edit_NoCheckpointOnValidationFailure(t *testing.T) {
	t.Parallel()
	cm := testContextManager(t)
	store := NewMemoryCheckpointStore()
	editor := NewMessageEditor(store, nil, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	baseline := append([]gateway.PromptMessage(nil), messages...)
	_, err := editor.Edit(context.Background(), "sess", "sess:0", &messages, 0, "nope", nil)
	if err == nil {
		t.Fatal("expected error when no post-anchor turns exist")
	}
	messages = append([]gateway.PromptMessage(nil), baseline...)
	result, err := editor.Edit(context.Background(), "sess", "sess:1", &messages, EditAnchorUserTurn, "task v2", nil)
	if err != nil {
		t.Fatalf("valid Edit: %v", err)
	}
	if result.CheckpointID != "sess:cp:1" {
		t.Fatalf("checkpoint id = %q, want sess:cp:1 (failed edit must not consume an id)", result.CheckpointID)
	}
}

func TestMessageEditor_Edit_RejectsInvalidTurnIndex(t *testing.T) {
	t.Parallel()
	cm := testContextManager(t)
	editor := NewMessageEditor(NewMemoryCheckpointStore(), nil, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	_, err := editor.Edit(context.Background(), "sess", "sess:0", &messages, 0, "nope", nil)
	if err == nil {
		t.Fatal("expected error when no post-anchor turns exist")
	}
	if !strings.Contains(err.Error(), "turn index") {
		t.Fatalf("error = %v, want turn index error", err)
	}
}

func TestMessageEditor_Edit_RejectsAssistantOnlyTurn(t *testing.T) {
	t.Parallel()
	cm := testContextManager(t)
	editor := NewMessageEditor(NewMemoryCheckpointStore(), nil, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "only assistant in rest"},
	}
	_, err := editor.Edit(context.Background(), "sess", "sess:0", &messages, 0, "nope", nil)
	if err == nil {
		t.Fatal("expected error for assistant-only editable turn")
	}
	if !strings.Contains(err.Error(), "editable user message") {
		t.Fatalf("error = %v, want non-editable turn error", err)
	}
}

func TestMessageEditor_Edit_AuditRecord(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger := NewAuditLogger(NewFileAuditSink(path), true)
	cm := testContextManager(t)
	editor := NewMessageEditor(NewMemoryCheckpointStore(), logger, cm)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "x"},
	}
	_, err := editor.Edit(context.Background(), "sess", "turn-1", &messages, EditAnchorUserTurn, "new", nil)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	line := string(data)
	if !strings.Contains(line, "history_edit") {
		t.Fatalf("audit line missing history_edit: %q", line)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &rec); err != nil {
		t.Fatalf("unmarshal audit: %v", err)
	}
	if rec["new_content_hash"] == nil || rec["new_content_hash"] == "" {
		t.Fatalf("missing new_content_hash in %v", rec)
	}
}
