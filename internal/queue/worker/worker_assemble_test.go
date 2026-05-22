package worker

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestAssembleAgenticSystemPrompt_Basic(t *testing.T) {
	w := &Worker{}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Fix bug",
		Description: "Fix the login bug",
	}
	project := models.Project{}
	profile := models.AgentProfile{}

	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Fatalf("expected first message role system, got %s", messages[0].Role)
	}
	if !strings.Contains(messages[0].Content, "autonomous agent") {
		t.Fatal("system prompt missing agentic text")
	}
	if messages[1].Role != "user" {
		t.Fatalf("expected second message role user, got %s", messages[1].Role)
	}
	if !strings.Contains(messages[1].Content, "Fix bug") {
		t.Fatal("user message missing task title")
	}
}

func TestAssembleAgenticSystemPrompt_WithTaskSystemPrompt(t *testing.T) {
	w := &Worker{}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Fix bug",
		Description: "Fix the login bug",
	}
	project := models.Project{}
	profile := models.AgentProfile{
		SystemPrompt: sql.NullString{String: "Be concise", Valid: true},
	}

	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if !strings.Contains(messages[0].Content, "Be concise") {
		t.Fatal("system prompt missing task-level override")
	}
	if !strings.Contains(messages[0].Content, "autonomous agent") {
		t.Fatal("system prompt missing global agentic text")
	}
	// Task-level should come after global in the builder output
	globalIdx := strings.Index(messages[0].Content, "autonomous agent")
	taskIdx := strings.Index(messages[0].Content, "Be concise")
	if globalIdx == -1 || taskIdx == -1 || taskIdx < globalIdx {
		t.Fatal("task-level prompt should appear after global in assembled output")
	}
}

func writeInstructionFixtures(t *testing.T, dir string) string {
	t.Helper()
	agentsMD := `# Agent Instructions

## Architecture
Use hexagonal architecture.

## Conventions
Always write tests first.

## Known Hazards
Never commit secrets.

## Agent Scope
Backend services only.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	prefsPath := filepath.Join(dir, "prefs.yaml")
	if err := os.WriteFile(prefsPath, []byte(`preferences:
  style_guide: "Use American English"`), 0o644); err != nil {
		t.Fatal(err)
	}
	return prefsPath
}

func TestAssembleAgenticSystemPrompt_WithInstructions(t *testing.T) {
	dir := t.TempDir()
	prefsPath := writeInstructionFixtures(t, dir)
	w := &Worker{instructionLoader: &InstructionLoader{ProjectFile: "AGENTS.md", UserPreferencesPath: prefsPath}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, Title: "Fix bug", Description: "Fix the login bug"}
	messages := w.assembleAgenticSystemPrompt(context.Background(), task, models.Project{WorkspacePath: dir}, models.AgentProfile{})
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	assertInstructionPromptContent(t, messages[0].Content)
}

func assertInstructionPromptContent(t *testing.T, content string) {
	t.Helper()
	for _, want := range []string{
		"Use American English", "hexagonal architecture", "write tests first",
		"Never commit secrets", "task-level overrides matched-skills",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in prompt", want)
		}
	}
}

func TestAssembleAgenticSystemPrompt_WithSkills(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".agentd", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillContent := `# Skill: Deploy

## When This Applies
Deployment, CI/CD, release

## The Procedure
Run tests, build, deploy
`
	if err := os.WriteFile(filepath.Join(skillsDir, "deploy.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	w := &Worker{
		skillLoader: &SkillLoader{ProjectDir: ".agentd/skills"},
		skillRouter: &SkillRouter{Threshold: 0.0, TopK: 3},
	}

	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Deploy",
		Description: "Deploy the release to production",
	}
	project := models.Project{WorkspacePath: dir}
	profile := models.AgentProfile{}

	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	content := messages[0].Content
	if !strings.Contains(content, "=== Skill: Deploy ===") {
		t.Fatal("missing matched skill in prompt")
	}
	if !strings.Contains(content, "Run tests, build, deploy") {
		t.Fatal("missing skill procedure in prompt")
	}
}

func TestBuildSystemPromptContent_GlobalSkillsEmptyWorkspace(t *testing.T) {
	globalDir := t.TempDir()
	skillContent := "# Skill: Logging\n\n## When This Applies\n\nlogging, observability\n\n## The Procedure\n\nUse structured logs\n"
	if err := os.WriteFile(filepath.Join(globalDir, "logging.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	w := &Worker{
		skillLoader: &SkillLoader{GlobalDir: globalDir},
		skillRouter: &SkillRouter{Threshold: 0.0, TopK: 3},
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Add logging",
		Description: "Improve observability with structured logging",
	}

	prompt := w.buildSystemPromptContent(task, models.Project{}, models.AgentProfile{})
	if !strings.Contains(prompt, "=== Skill: Logging ===") {
		t.Fatalf("missing global skill in prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "Use structured logs") {
		t.Fatal("missing skill procedure in prompt")
	}
}

func TestAssembleAgenticSystemPrompt_WithMemoryLessons(t *testing.T) {
	w := &Worker{
		retriever: &mockMemoryRetriever{
			memories: []models.Memory{
				{
					Scope:    "LESSON",
					Symptom:  sql.NullString{String: "Flaky test", Valid: true},
					Solution: sql.NullString{String: "Add retry", Valid: true},
				},
			},
		},
	}

	task := models.Task{
		BaseEntity: models.BaseEntity{
			ID: "t1",
		},
		Title:       "Fix bug",
		Description: "Fix the login bug",
	}
	project := models.Project{}
	profile := models.AgentProfile{}

	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (lessons + system + user), got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Fatalf("expected first message role system, got %s", messages[0].Role)
	}
	if !strings.Contains(messages[0].Content, "LESSONS LEARNED") {
		t.Fatal("missing memory lessons")
	}
	if messages[1].Role != "system" {
		t.Fatalf("expected second message role system, got %s", messages[1].Role)
	}
	if messages[2].Role != "user" {
		t.Fatalf("expected third message role user, got %s", messages[2].Role)
	}
}

func TestAssembleAgenticSystemPrompt_MissingFilesAreNonFatal(t *testing.T) {
	w := &Worker{
		instructionLoader: &InstructionLoader{
			ProjectFile:         ".agentd/AGENTS.md",
			UserPreferencesPath: "/nonexistent/prefs.yaml",
		},
		skillLoader: &SkillLoader{
			ProjectDir: ".agentd/skills",
			GlobalDir:  "/nonexistent/skills",
		},
		skillRouter: &SkillRouter{Threshold: 0.1, TopK: 3},
	}

	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Fix bug",
		Description: "Fix the login bug",
	}
	project := models.Project{WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{}

	// Should not panic or error even when files are missing
	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	// Should still have the default agentic system prompt
	if !strings.Contains(messages[0].Content, "autonomous agent") {
		t.Fatal("missing default agentic text when files missing")
	}
}

func TestAssembleAgenticSystemPrompt_NilPromptLibrary(t *testing.T) {
	w := &Worker{promptLibrary: nil}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t-nil-lib"},
		Title:       "Implement add",
		Description: "Add function in math.go\nSignature:\nfunc Add(a, b int) int",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeCodeGen}
	messages := w.assembleAgenticSystemPrompt(context.Background(), task, models.Project{}, profile)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if !strings.Contains(messages[1].Content, "You are executing Task:") {
		t.Fatalf("nil promptLibrary should use default user prompt, got %q", messages[1].Content)
	}
}

func TestAssembleAgenticSystemPrompt_CodeGenTemplate(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{promptLibrary: lib}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t-codegen"},
		Title:       "Implement add",
		Description: "Add function in math.go\nSignature:\nfunc Add(a, b int) int\nTest cases:\n- Add(1,2) == 3",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeCodeGen}
	messages := w.assembleAgenticSystemPrompt(context.Background(), task, models.Project{}, profile)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	user := messages[1].Content
	if strings.Contains(user, "You are executing Task:") {
		t.Fatalf("code_gen should use template user prompt, got %q", user)
	}
	if !strings.Contains(user, "math.go") || !strings.Contains(user, "func Add") {
		t.Fatalf("template user missing structured slots: %q", user)
	}
	if !strings.Contains(messages[0].Content, "autonomous agent") {
		t.Fatal("system should include instruction hierarchy prefix")
	}
	if !strings.Contains(messages[0].Content, "raw source code") {
		t.Fatal("system should include CODE_PROMPT_BUILDER template")
	}
}

type mockMemoryRetriever struct {
	memories []models.Memory
}

func (m *mockMemoryRetriever) Recall(_ context.Context, _, _, _ string) []models.Memory {
	return m.memories
}
