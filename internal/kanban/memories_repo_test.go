package kanban

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/memory"
	"agentd/internal/models"
)

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
	formatted := memory.FormatPreferences([]models.Memory{recalled1[0]})
	if !strings.Contains(formatted, "use bullet format only") {
		t.Fatalf("FormatPreferences output = %q, want to include the solution", formatted)
	}
}

// TestUserPreferenceRecallNotStarvedByTopK guards the J11 finding: preferences
// used to be a branch of the bm25-ranked FTS query, so they competed for the
// same top-K as lessons. Once a user had more preferences than RecallTopK,
// the newest were silently dropped and never reached the worker's prompt —
// exactly what the J11 journey hit on its second run.
func TestUserPreferenceRecallNotStarvedByTopK(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const userID = "user-prolific"
	const total = 8 // more than the default top-K of 5

	for i := 0; i < total; i++ {
		pref := models.Memory{
			Scope:    "USER_PREFERENCE",
			Tags:     sql.NullString{String: "user_id:" + userID, Valid: true},
			Symptom:  sql.NullString{String: "preference", Valid: true},
			Solution: sql.NullString{String: fmt.Sprintf("preference number %d", i), Valid: true},
		}
		if err := store.RecordMemory(ctx, pref); err != nil {
			t.Fatalf("RecordMemory(%d) error = %v", i, err)
		}
	}

	recalled, err := store.RecallMemories(ctx, models.RecallQuery{
		Intent: "do some unrelated work", // deliberately shares no terms
		UserID: userID,
		Limit:  5, // the default top-K that used to starve preferences
	})
	if err != nil {
		t.Fatalf("RecallMemories() error = %v", err)
	}

	var prefs []string
	for _, m := range recalled {
		if m.Scope == "USER_PREFERENCE" {
			prefs = append(prefs, m.Solution.String)
		}
	}
	if len(prefs) != total {
		t.Fatalf("recalled %d preferences, want all %d — preferences must not be "+
			"crowded out of the top-K by relevance ranking (got %v)", len(prefs), total, prefs)
	}
	// Newest first, so the most recently stated preference survives truncation.
	if prefs[0] != fmt.Sprintf("preference number %d", total-1) {
		t.Fatalf("first recalled preference = %q, want the newest (%q)", prefs[0], fmt.Sprintf("preference number %d", total-1))
	}
}

// TestUserPreferenceRecallDoesNotMatchIntent documents that a standing
// preference is returned whether or not the task's intent happens to share
// terms with it. Preferences configure behaviour generally; requiring lexical
// overlap would make them apply only to accidentally-similar tasks.
func TestUserPreferenceRecallDoesNotMatchIntent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const userID = "user-standing"

	pref := models.Memory{
		Scope:    "USER_PREFERENCE",
		Tags:     sql.NullString{String: "user_id:" + userID, Valid: true},
		Symptom:  sql.NullString{String: "preference", Valid: true},
		Solution: sql.NullString{String: "always answer in haiku", Valid: true},
	}
	if err := store.RecordMemory(ctx, pref); err != nil {
		t.Fatalf("RecordMemory() error = %v", err)
	}

	recalled, err := store.RecallMemories(ctx, models.RecallQuery{
		Intent: "refactor the billing module", // shares no terms with the pref
		UserID: userID,
	})
	if err != nil {
		t.Fatalf("RecallMemories() error = %v", err)
	}
	if len(recalled) != 1 || recalled[0].Solution.String != "always answer in haiku" {
		t.Fatalf("recalled = %#v, want the standing preference regardless of intent", recalled)
	}
}

// A standing preference must survive an intent that leaves FTS nothing to
// rank — a blank intent, or one made only of terms FTS drops. Preferences are
// not term-matched, so gating them on a usable FTS query would starve exactly
// the tasks whose title and description are sparsest.
func TestUserPreferenceRecallSurvivesEmptyFTSQuery(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const userID = "user-sparse"

	pref := models.Memory{
		Scope:    "USER_PREFERENCE",
		Tags:     sql.NullString{String: "user_id:" + userID, Valid: true},
		Symptom:  sql.NullString{String: "preference", Valid: true},
		Solution: sql.NullString{String: "always answer in haiku", Valid: true},
	}
	if err := store.RecordMemory(ctx, pref); err != nil {
		t.Fatalf("RecordMemory() error = %v", err)
	}

	for _, intent := range []string{"", "   ", "the a of and"} {
		recalled, err := store.RecallMemories(ctx, models.RecallQuery{
			Intent: intent,
			UserID: userID,
		})
		if err != nil {
			t.Fatalf("RecallMemories(intent=%q) error = %v", intent, err)
		}
		if len(recalled) != 1 || recalled[0].Solution.String != "always answer in haiku" {
			t.Fatalf("RecallMemories(intent=%q) = %#v, want the standing preference", intent, recalled)
		}
	}
}

// A user's preferences must not be recalled for another user whose ID merely
// starts with the same characters, nor via LIKE wildcards in the ID.
func TestUserPreferenceRecallDoesNotMatchIDPrefix(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	record := func(tags, solution string) {
		t.Helper()
		err := store.RecordMemory(ctx, models.Memory{
			Scope:    "USER_PREFERENCE",
			Tags:     sql.NullString{String: tags, Valid: true},
			Symptom:  sql.NullString{String: "preference", Valid: true},
			Solution: sql.NullString{String: solution, Valid: true},
		})
		if err != nil {
			t.Fatalf("RecordMemory(%q) error = %v", tags, err)
		}
	}
	record("user_id:alice2,pref", "alice2 private preference")
	record("user_id:alice", "alice own preference")

	for _, userID := range []string{"alice", "ali%", "alic_"} {
		got, err := store.RecallMemories(ctx, models.RecallQuery{Intent: "unrelated", UserID: userID})
		if err != nil {
			t.Fatalf("RecallMemories(%q) error = %v", userID, err)
		}
		for _, m := range got {
			if m.Solution.String == "alice2 private preference" {
				t.Fatalf("RecallMemories(%q) leaked alice2's preference", userID)
			}
			if userID != "alice" && m.Solution.String == "alice own preference" {
				t.Fatalf("RecallMemories(%q) matched alice's preference via wildcard", userID)
			}
		}
	}
}
