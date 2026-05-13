package worker

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestParseAgentsMD(t *testing.T) {
	content := `
# Agent Instructions

## Architecture
This is the architecture section.
It spans multiple lines.

## Conventions
- Use Go
- Use slog

## Known Hazards
Don't delete the database.

## Agent Scope
You can do everything.

## Unrelated
This should be ignored.
`
	pi := parseAgentsMD(content)

	if pi.Architecture != "This is the architecture section.\nIt spans multiple lines." {
		t.Errorf("unexpected architecture: %q", pi.Architecture)
	}
	if pi.Conventions != "- Use Go\n- Use slog" {
		t.Errorf("unexpected conventions: %q", pi.Conventions)
	}
	if pi.KnownHazards != "Don't delete the database." {
		t.Errorf("unexpected known hazards: %q", pi.KnownHazards)
	}
	if pi.AgentScope != "You can do everything." {
		t.Errorf("unexpected agent scope: %q", pi.AgentScope)
	}
	if pi.Raw != content {
		t.Errorf("unexpected raw content")
	}
}

func TestLoadProjectInstructions(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "repo")
	os.MkdirAll(filepath.Join(workspace, ".agentd"), 0755)

	agentsContent := "## Architecture\nProject architecture."
	os.WriteFile(filepath.Join(workspace, ".agentd", "AGENTS.md"), []byte(agentsContent), 0644)

	loader := &InstructionLoader{
		ProjectFile: ".agentd/AGENTS.md",
	}

	// 1. Default path
	pi, err := loader.LoadProjectInstructions(workspace, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi == nil || pi.Architecture != "Project architecture." {
		t.Fatalf("failed to load default project instructions: %+v", pi)
	}

	// 2. Override path
	os.WriteFile(filepath.Join(workspace, "CUSTOM.md"), []byte("## Architecture\nCustom architecture."), 0644)
	pi, err = loader.LoadProjectInstructions(workspace, "CUSTOM.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi == nil || pi.Architecture != "Custom architecture." {
		t.Fatalf("failed to load custom project instructions: %+v", pi)
	}

	// 3. Fallback to root AGENTS.md
	os.Remove(filepath.Join(workspace, ".agentd", "AGENTS.md"))
	os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("## Architecture\nRoot architecture."), 0644)
	pi, err = loader.LoadProjectInstructions(workspace, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi == nil || pi.Architecture != "Root architecture." {
		t.Fatalf("failed to fallback to root AGENTS.md: %+v", pi)
	}

	// 4. No file
	os.Remove(filepath.Join(workspace, "AGENTS.md"))
	pi, err = loader.LoadProjectInstructions(workspace, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi != nil {
		t.Fatalf("expected nil when no file exists")
	}
}

func TestLoadUserPreferences(t *testing.T) {
	tmp := t.TempDir()
	prefsPath := filepath.Join(tmp, "prefs.yaml")
	
	content := `
preferences:
  style: concise
  editor: vim
`
	os.WriteFile(prefsPath, []byte(content), 0644)

	loader := &InstructionLoader{
		UserPreferencesPath: prefsPath,
	}

	prefs, err := loader.LoadUserPreferences()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefs == nil || len(prefs.Entries) != 2 {
		t.Fatalf("failed to load user preferences: %+v", prefs)
	}
	if prefs.Entries["style"] != "concise" || prefs.Entries["editor"] != "vim" {
		t.Errorf("unexpected preference values")
	}

	// Test missing file
	loader.UserPreferencesPath = filepath.Join(tmp, "missing.yaml")
	prefs, err = loader.LoadUserPreferences()
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if prefs != nil {
		t.Fatalf("expected nil for missing file")
	}
}

func TestSystemPromptBuilder(t *testing.T) {
	builder := NewSystemPromptBuilder()
	
	// 1. Global only
	builder.WithGlobal("Global prompt.")
	got := builder.Build()
	if !contains(got, "Global prompt.") || !contains(got, resolutionRule) {
		t.Errorf("missing components in global-only: %q", got)
	}

	// 2. All layers
	pi := &ProjectInstructions{
		KnownHazards: "Hazards text.",
		AgentScope:   "Scope text.",
		Architecture: "Arch text.",
		Conventions:  "Conv text.",
	}
	prefs := &UserPreferences{
		Entries: map[string]string{"key": "val"},
	}
	
	builder.WithProject(pi).
		WithTask("Task prompt.").
		WithUserPreferences(prefs)
	
	got = builder.Build()
	
	expected := []string{
		"User Preferences", "key: val",
		"KNOWN HAZARDS", "Hazards text.",
		"Global prompt.",
		"AGENT SCOPE", "Scope text.",
		"Task prompt.",
		"ARCHITECTURE", "Arch text.",
		"CONVENTIONS", "Conv text.",
		resolutionRule,
	}
	
	for _, exp := range expected {
		if !contains(got, exp) {
			t.Errorf("missing expected component %q in: %q", exp, got)
		}
	}
	
	// Precedence order check (basic)
	hazIdx := index(got, "KNOWN HAZARDS")
	globIdx := index(got, "Global prompt.")
	scopeIdx := index(got, "AGENT SCOPE")
	taskIdx := index(got, "Task prompt.")
	archIdx := index(got, "ARCHITECTURE")
	
	if hazIdx > globIdx || globIdx > scopeIdx || scopeIdx > archIdx || archIdx > taskIdx {
		t.Errorf("incorrect order: hazards=%d, global=%d, scope=%d, arch=%d, task=%d", hazIdx, globIdx, scopeIdx, archIdx, taskIdx)
	}
}

func TestBuildAgenticMessagesHierarchy(t *testing.T) {
	w := &Worker{}
	messages := []gateway.PromptMessage{
		{Role: "user", Content: "hello"},
	}
	
	profile := models.AgentProfile{
		SystemPrompt: sql.NullString{String: "Task custom.", Valid: true},
	}
	
	pi := &ProjectInstructions{
		Architecture: "Project arch.",
	}
	
	prefs := &UserPreferences{
		Entries: map[string]string{"pref": "val"},
	}
	
	got := w.buildAgenticMessages(messages, profile, pi, prefs)
	
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	
	content := got[0].Content
	if !contains(content, "Task custom.") {
		t.Errorf("missing task prompt")
	}
	if !contains(content, "Project arch.") {
		t.Errorf("missing project instructions")
	}
	if !contains(content, "pref: val") {
		t.Errorf("missing user preferences")
	}
	if !contains(content, resolutionRule) {
		t.Errorf("missing resolution rule")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func index(s, substr string) int {
	return strings.Index(s, substr)
}
