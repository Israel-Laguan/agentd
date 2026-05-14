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

func TestAssembleAgenticSystemPrompt_WithInstructions(t *testing.T) {
	dir := t.TempDir()

	// Write AGENTS.md
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
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(agentsMD), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write user preferences
	prefsPath := filepath.Join(dir, "prefs.yaml")
	prefsContent := `preferences:
  style_guide: "Use American English"`
	if err := os.WriteFile(prefsPath, []byte(prefsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	w := &Worker{
		instructionLoader: &InstructionLoader{
			ProjectFile:         "AGENTS.md",
			UserPreferencesPath: prefsPath,
		},
	}

	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Fix bug",
		Description: "Fix the login bug",
	}
	project := models.Project{WorkspacePath: dir}
	profile := models.AgentProfile{}

	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	content := messages[0].Content

	// User preferences should appear
	if !strings.Contains(content, "Use American English") {
		t.Fatal("missing user preferences in prompt")
	}

	// Project instructions should appear
	if !strings.Contains(content, "hexagonal architecture") {
		t.Fatal("missing architecture section from AGENTS.md")
	}
	if !strings.Contains(content, "write tests first") {
		t.Fatal("missing conventions section from AGENTS.md")
	}
	if !strings.Contains(content, "Never commit secrets") {
		t.Fatal("missing known hazards section from AGENTS.md")
	}

	// Resolution rule should appear
	if !strings.Contains(content, "task-level overrides matched-skills") {
		t.Fatal("missing resolution rule")
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

type mockMemoryRetriever struct {
	memories []models.Memory
}

func (m *mockMemoryRetriever) Recall(_ context.Context, _, _, _ string) []models.Memory {
	return m.memories
}
