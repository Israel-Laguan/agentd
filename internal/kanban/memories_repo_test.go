package kanban

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agentd/internal/memory"
	"agentd/internal/models"
)

// FormatPreferences wraps the recall.FormatPreferences for test use.
func FormatPreferences(memories []models.Memory) string {
	return memory.FormatPreferences(memories)
}

func TestRecordAndListMemories(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject() error = %v", err)
	}
	if err := recordGlobalMemory(ctx, store); err != nil {
		t.Fatalf("RecordMemory(global) error = %v", err)
	}
	if err := recordProjectMemory(ctx, store, project.ID); err != nil {
		t.Fatalf("RecordMemory(project) error = %v", err)
	}
	assertGlobalMemory(t, ctx, store)
	assertProjectMemory(t, ctx, store, project.ID)
}

func recordGlobalMemory(ctx context.Context, store *Store) error {
	return store.RecordMemory(ctx, models.Memory{
		Scope: "GLOBAL",
		Tags: sql.NullString{
			String: "reboot,recovery",
			Valid:  true,
		},
		Symptom: sql.NullString{
			String: "daemon_reboot_interrupted_tasks",
			Valid:  true,
		},
		Solution: sql.NullString{
			String: "task was reset after daemon startup",
			Valid:  true,
		},
	})
}

func recordProjectMemory(ctx context.Context, store *Store, projectID string) error {
	return store.RecordMemory(ctx, models.Memory{
		Scope: "PROJECT",
		ProjectID: sql.NullString{
			String: projectID,
			Valid:  true,
		},
		Symptom: sql.NullString{
			String: "project-specific symptom",
			Valid:  true,
		},
	})
}

func assertGlobalMemory(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	global, err := store.ListMemories(ctx, models.MemoryFilter{Scope: "GLOBAL"})
	if err != nil {
		t.Fatalf("ListMemories(global) error = %v", err)
	}
	if len(global) != 1 || global[0].Symptom.String != "daemon_reboot_interrupted_tasks" {
		t.Fatalf("global memories = %#v, want reboot memory", global)
	}
}

func TestMemoryFTSLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	memID := seedRebootMemory(t, store, ctx)
	newID := recordMergedMemory(t, store, ctx)
	assertMemorySuperseded(t, store, ctx, memID, newID)
	assertMemoryNoOps(t, store, ctx, newID)
}

func seedRebootMemory(t *testing.T, store *Store, ctx context.Context) string {
	t.Helper()
	m := models.Memory{
		Scope:    models.MemoryScopeGlobal,
		Symptom:  sql.NullString{String: "daemon reboot interrupted tasks", Valid: true},
		Solution: sql.NullString{String: "reset task after startup", Valid: true},
	}
	if err := store.RecordMemory(ctx, m); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	recalled, err := store.RecallMemories(ctx, models.RecallQuery{Intent: "reboot interrupted"})
	if err != nil {
		t.Fatalf("RecallMemories: %v", err)
	}
	if len(recalled) == 0 {
		t.Fatal("RecallMemories returned no matches")
	}
	if err := store.TouchMemories(ctx, []string{recalled[0].ID}); err != nil {
		t.Fatalf("TouchMemories: %v", err)
	}
	return recalled[0].ID
}

func recordMergedMemory(t *testing.T, store *Store, ctx context.Context) string {
	t.Helper()
	newMem := models.Memory{
		Scope:    models.MemoryScopeGlobal,
		Symptom:  sql.NullString{String: "daemon reboot interrupted tasks", Valid: true},
		Solution: sql.NullString{String: "merged recovery guidance", Valid: true},
	}
	if err := store.RecordMemory(ctx, newMem); err != nil {
		t.Fatalf("RecordMemory merge target: %v", err)
	}
	all, err := store.ListUnsupersededMemories(ctx)
	if err != nil {
		t.Fatalf("ListUnsupersededMemories: %v", err)
	}
	for _, mem := range all {
		if mem.Solution.String == "merged recovery guidance" {
			return mem.ID
		}
	}
	t.Fatal("merge target memory not found")
	return ""
}

func assertMemorySuperseded(t *testing.T, store *Store, ctx context.Context, memID, newID string) {
	t.Helper()
	if err := store.SupersedeMemories(ctx, []string{memID}, newID); err != nil {
		t.Fatalf("SupersedeMemories: %v", err)
	}
	active, err := store.ListUnsupersededMemories(ctx)
	if err != nil {
		t.Fatalf("ListUnsupersededMemories after supersede: %v", err)
	}
	for _, mem := range active {
		if mem.ID == memID {
			t.Fatalf("superseded memory still active: %+v", mem)
		}
	}
}

func assertMemoryNoOps(t *testing.T, store *Store, ctx context.Context, newID string) {
	t.Helper()
	if got, err := store.RecallMemories(ctx, models.RecallQuery{Intent: ""}); err != nil || got != nil {
		t.Fatalf("empty intent: got=%v err=%v", got, err)
	}
	if err := store.TouchMemories(ctx, nil); err != nil {
		t.Fatalf("TouchMemories nil: %v", err)
	}
	if err := store.SupersedeMemories(ctx, nil, newID); err != nil {
		t.Fatalf("SupersedeMemories nil: %v", err)
	}
}

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		intent string
		want   string
	}{
		{intent: "fix cors error", want: "fix OR cors OR error"},
		{intent: "a", want: ""},
		{intent: "foo!!!", want: "foo"},
		{intent: "", want: ""},
	}
	for _, tt := range tests {
		if got := buildFTSQuery(tt.intent); got != tt.want {
			t.Fatalf("buildFTSQuery(%q) = %q, want %q", tt.intent, got, tt.want)
		}
	}
}

func assertProjectMemory(t *testing.T, ctx context.Context, store *Store, projectID string) {
	t.Helper()
	projectMemories, err := store.ListMemories(ctx, models.MemoryFilter{
		Scope: "PROJECT",
		ProjectID: sql.NullString{
			String: projectID,
			Valid:  true,
		},
	})
	if err != nil {
		t.Fatalf("ListMemories(project) error = %v", err)
	}
	if len(projectMemories) != 1 || projectMemories[0].ProjectID.String != projectID {
		t.Fatalf("project memories = %#v, want one project-scoped memory", projectMemories)
	}
}

// TestUserPreferenceRecall tests T-025 Part A: a USER_PREFERENCE memory is
// recalled by user and intent on real SQLite, isn't returned for another user,
// and appears in FormatPreferences output.
func TestUserPreferenceRecall(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID1 := "user-alice"
	userID2 := "user-bob"

	// Record a USER_PREFERENCE for user 1 (user_id encoded in Tags)
	prefMemory := models.Memory{
		Scope:    "USER_PREFERENCE",
		Tags:     sql.NullString{String: "user_id:" + userID1 + ",pref", Valid: true},
		Symptom:  sql.NullString{String: "format preference: compact lists", Valid: true},
		Solution: sql.NullString{String: "use bullet format only", Valid: true},
	}
	if err := store.RecordMemory(ctx, prefMemory); err != nil {
		t.Fatalf("RecordMemory() error = %v", err)
	}

	// Recall by user 1 should find the preference
	recalled1, err := store.RecallMemories(ctx, models.RecallQuery{
		Intent: "format preference",
		UserID: userID1,
	})
	if err != nil {
		t.Fatalf("RecallMemories(user1) error = %v", err)
	}
	if len(recalled1) == 0 {
		t.Fatal("RecallMemories(user1) returned no matches, want the recorded preference")
	}
	if recalled1[0].Solution.String != "use bullet format only" {
		t.Fatalf("recalled solution = %q, want bullet format", recalled1[0].Solution.String)
	}

	// Recall by user 2 should NOT find user 1's preference
	recalled2, err := store.RecallMemories(ctx, models.RecallQuery{
		Intent: "format preference",
		UserID: userID2,
	})
	if err != nil {
		t.Fatalf("RecallMemories(user2) error = %v", err)
	}
	if len(recalled2) > 0 {
		t.Fatalf("RecallMemories(user2) returned %d memories, want 0 (user1 pref should not leak)", len(recalled2))
	}

	// FormatPreferences should include the preference
	formatted := FormatPreferences([]models.Memory{recalled1[0]})
	if !strings.Contains(formatted, "use bullet format only") {
		t.Fatalf("FormatPreferences output = %q, want to include the solution", formatted)
	}
}
