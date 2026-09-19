package worker

import (
	"database/sql"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestIsTieredStep(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	tests := []struct {
		name  string
		agent string
		want  bool
	}{
		{"context step", tieredStepProfile[TieredStepContext], true},
		{"decision step", tieredStepProfile[TieredStepDecision], true},
		{"execute step", tieredStepProfile[TieredStepExecute], true},
		{"verify step", tieredStepProfile[TieredStepVerify], true},
		{"regular task", "default", false},
		{"empty agent", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := models.Task{AgentID: tt.agent}
			if got := w.isTieredStep(task); got != tt.want {
				t.Errorf("isTieredStep() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTieredStepKind(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	tests := []struct {
		agent string
		want  TieredStepKind
	}{
		{tieredStepProfile[TieredStepContext], TieredStepContext},
		{tieredStepProfile[TieredStepDecision], TieredStepDecision},
		{tieredStepProfile[TieredStepExecute], TieredStepExecute},
		{tieredStepProfile[TieredStepVerify], TieredStepVerify},
		{"unknown", TieredStepContext}, // default
	}
	for _, tt := range tests {
		task := models.Task{AgentID: tt.agent}
		if got := w.tieredStepKind(task); got != tt.want {
			t.Errorf("tieredStepKind() = %v, want %v", got, tt.want)
		}
	}
}

func TestApplyTieredStepProfile_ToolAllowlists(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	profile := models.AgentProfile{
		ID:           "base",
		AgenticMode:  false,
		AllowedTools: nil,
	}
	tests := []struct {
		kind      TieredStepKind
		wantTools []string
	}{
		{TieredStepContext, []string{"read", "grep", "glob", "list"}},
		{TieredStepDecision, []string{"read", "grep", "glob", "list"}},
		{TieredStepExecute, []string{"read", "write", "bash", "grep", "glob"}},
		{TieredStepVerify, []string{"read", "bash", "grep", "glob"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := w.applyTieredStepProfile(profile, tt.kind)
			if len(got.AllowedTools) != len(tt.wantTools) {
				t.Fatalf("AllowedTools len = %d, want %d", len(got.AllowedTools), len(tt.wantTools))
			}
			for i, tool := range got.AllowedTools {
				if tool != tt.wantTools[i] {
					t.Errorf("AllowedTools[%d] = %q, want %q", i, tool, tt.wantTools[i])
				}
			}
		})
	}
}

func TestApplyTieredStepProfile_SystemPromptSuffix(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	profile := models.AgentProfile{
		ID:           "base",
		SystemPrompt: sql.NullString{String: "Base prompt.", Valid: true},
	}
	got := w.applyTieredStepProfile(profile, TieredStepContext)
	if !got.SystemPrompt.Valid {
		t.Fatal("SystemPrompt.Valid = false, want true")
	}
	if got.SystemPrompt.String == "Base prompt." {
		t.Error("SystemPrompt was not modified with step suffix")
	}
}

func TestApplyTieredStepProfile_SystemPromptFromEmpty(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	profile := models.AgentProfile{
		ID:           "base",
		SystemPrompt: sql.NullString{String: "", Valid: false},
	}
	got := w.applyTieredStepProfile(profile, TieredStepContext)
	if !got.SystemPrompt.Valid {
		t.Fatal("SystemPrompt.Valid = false, want true after adding suffix")
	}
	if got.SystemPrompt.String == "" {
		t.Error("SystemPrompt is empty after adding suffix")
	}
}

func TestParseContextPack_ValidJSON(t *testing.T) {
	t.Parallel()
	input := `{"version":1,"task_id":"t1","summary":"test","paths":["a.go"],"budget":{"max_paths":40,"max_chars":48000,"path_count":1,"char_count":4}}`
	cp, err := parseContextPack(input)
	if err != nil {
		t.Fatalf("parseContextPack() error = %v", err)
	}
	if cp.TaskID != "t1" {
		t.Errorf("TaskID = %q, want %q", cp.TaskID, "t1")
	}
	if len(cp.Paths) != 1 || cp.Paths[0] != "a.go" {
		t.Errorf("Paths = %v, want [a.go]", cp.Paths)
	}
}

func TestParseContextPack_InCodeFence(t *testing.T) {
	t.Parallel()
	input := "Here is the context pack:\n```json\n{\"version\":1,\"task_id\":\"t2\",\"summary\":\"fenced\",\"paths\":[\"b.go\"],\"budget\":{\"max_paths\":40,\"max_chars\":48000,\"path_count\":1,\"char_count\":6}}\n```\nDone."
	cp, err := parseContextPack(input)
	if err != nil {
		t.Fatalf("parseContextPack() error = %v", err)
	}
	if cp.TaskID != "t2" {
		t.Errorf("TaskID = %q, want %q", cp.TaskID, "t2")
	}
}

func TestParseContextPack_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := parseContextPack("not json at all")
	if err == nil {
		t.Fatal("parseContextPack() should error on invalid JSON")
	}
}

func TestParseContextPack_EmptyFence(t *testing.T) {
	t.Parallel()
	_, err := parseContextPack("```json\n```")
	if err == nil {
		t.Fatal("parseContextPack() should error on empty JSON fence")
	}
}

func TestTieredStepToolAllowlists_CoversAllKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range tieredStepOrder {
		allowlist, ok := tieredStepToolAllowlists[kind]
		if !ok {
			t.Errorf("tieredStepToolAllowlists missing entry for %q", kind)
			continue
		}
		if len(allowlist) == 0 {
			t.Errorf("tieredStepToolAllowlists[%q] is empty", kind)
		}
	}
}

func TestTieredStepSystemPrompts_CoversAllKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range tieredStepOrder {
		prompt, ok := tieredStepSystemPrompts[kind]
		if !ok {
			t.Errorf("tieredStepSystemPrompts missing entry for %q", kind)
			continue
		}
		if len(prompt) == 0 {
			t.Errorf("tieredStepSystemPrompts[%q] is empty", kind)
		}
	}
}

func TestTieredStepProfiles_AllNonEmpty(t *testing.T) {
	t.Parallel()
	for _, kind := range tieredStepOrder {
		profile := tieredStepProfile[kind]
		if profile == "" {
			t.Errorf("tieredStepProfile[%q] is empty", kind)
		}
	}
}

func TestTieredStepProfiles_MatchesProfilesMap(t *testing.T) {
	t.Parallel()
	for kind, profileName := range tieredStepProfile {
		mappedKind, ok := tieredStepProfiles[profileName]
		if !ok {
			t.Errorf("tieredStepProfiles missing entry for profile %q", profileName)
			continue
		}
		if mappedKind != kind {
			t.Errorf("tieredStepProfiles[%q] = %v, want %v", profileName, mappedKind, kind)
		}
	}
}

func TestPersistTieredDAG_CreatesFourChildren(t *testing.T) {
	t.Parallel()
	parent := testTieredParent()
	children, _ := SplitIntoTieredDAG(parent, time.Now())
	if len(children) != 4 {
		t.Fatalf("len(children) = %d, want 4", len(children))
	}

	dagTasks := make([]models.TieredDAGTask, len(children))
	for i, child := range children {
		var dependsOnID string
		if i > 0 {
			dependsOnID = children[i-1].ID
		}
		dagTasks[i] = models.TieredDAGTask{
			Task:        child,
			DependsOnID: dependsOnID,
		}
	}

	if dagTasks[0].DependsOnID != "" {
		t.Errorf("first step DependsOnID = %q, want empty", dagTasks[0].DependsOnID)
	}
	for i := 1; i < len(dagTasks); i++ {
		if dagTasks[i].DependsOnID != children[i-1].ID {
			t.Errorf("step %d DependsOnID = %q, want %q", i, dagTasks[i].DependsOnID, children[i-1].ID)
		}
	}
}
