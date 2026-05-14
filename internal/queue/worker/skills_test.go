package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseSkillMD
// ---------------------------------------------------------------------------

func TestParseSkillMD_FullDocument(t *testing.T) {
	content := `# Skill: Database Migrations

## When This Applies

database migrations, schema changes, alembic, flyway

## The Procedure

1. Create a migration file.
2. Run the migration.

## Common Mistakes

- Forgetting to test rollback.

## Output Format

A migration SQL file.
`
	sk := parseSkillMD(content, "/tmp/migrations.md")
	if sk.Name != "Database Migrations" {
		t.Fatalf("Name = %q, want %q", sk.Name, "Database Migrations")
	}
	if sk.WhenApplies == "" {
		t.Fatal("WhenApplies should not be empty")
	}
	if !strings.Contains(sk.WhenApplies, "database migrations") {
		t.Fatalf("WhenApplies missing expected content: %q", sk.WhenApplies)
	}
	if sk.Procedure == "" {
		t.Fatal("Procedure should not be empty")
	}
	if sk.CommonMistakes == "" {
		t.Fatal("CommonMistakes should not be empty")
	}
	if sk.OutputFormat == "" {
		t.Fatal("OutputFormat should not be empty")
	}
	if sk.SourcePath != "/tmp/migrations.md" {
		t.Fatalf("SourcePath = %q, want %q", sk.SourcePath, "/tmp/migrations.md")
	}
}

func TestParseSkillMD_Empty(t *testing.T) {
	sk := parseSkillMD("", "/tmp/empty.md")
	if sk.Name != "" {
		t.Fatalf("Name = %q, want empty", sk.Name)
	}
	if !sk.IsEmpty() {
		t.Fatal("expected IsEmpty() = true for empty content")
	}
}

func TestParseSkillMD_NoSkillPrefix(t *testing.T) {
	content := "# Testing Guide\n\n## When This Applies\n\nunit tests\n"
	sk := parseSkillMD(content, "/tmp/test.md")
	if sk.Name != "Testing Guide" {
		t.Fatalf("Name = %q, want %q", sk.Name, "Testing Guide")
	}
}

// ---------------------------------------------------------------------------
// extractSkillName
// ---------------------------------------------------------------------------

func TestExtractSkillName_WithPrefix(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"standard", "# Skill: Code Review\n", "Code Review"},
		{"no space after colon", "# Skill:Code Review\n", "Code Review"},
		{"no prefix", "# Deployment\n", "Deployment"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSkillName(tt.content)
			if got != tt.want {
				t.Fatalf("extractSkillName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SkillLoader
// ---------------------------------------------------------------------------

func TestSkillLoader_LoadAll_ProjectDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".agentd", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Skill: Testing\n\n## When This Applies\n\nunit tests\n"
	if err := os.WriteFile(filepath.Join(skillDir, "testing.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := &SkillLoader{ProjectDir: ".agentd/skills/"}
	skills, err := loader.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}
	if skills[0].Name != "Testing" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "Testing")
	}
}

func TestSkillLoader_LoadAll_GlobalDir(t *testing.T) {
	globalDir := t.TempDir()
	content := "# Skill: Logging\n\n## When This Applies\n\nlogging, observability\n"
	if err := os.WriteFile(filepath.Join(globalDir, "logging.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := &SkillLoader{GlobalDir: globalDir}
	skills, err := loader.LoadAll("")
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}
	if skills[0].Name != "Logging" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "Logging")
	}
}

func TestSkillLoader_LoadAll_ProjectShadowsGlobal(t *testing.T) {
	projDir := t.TempDir()
	globalDir := t.TempDir()

	skillDir := filepath.Join(projDir, ".agentd", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	projectContent := "# Skill: Deploy\n\n## When This Applies\n\nproject deploy\n"
	globalContent := "# Skill: Deploy\n\n## When This Applies\n\nglobal deploy\n"

	if err := os.WriteFile(filepath.Join(skillDir, "deploy.md"), []byte(projectContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "deploy.md"), []byte(globalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := &SkillLoader{ProjectDir: ".agentd/skills/", GlobalDir: globalDir}
	skills, err := loader.LoadAll(projDir)
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1 (project should shadow global)", len(skills))
	}
	if !strings.Contains(skills[0].WhenApplies, "project deploy") {
		t.Fatalf("expected project skill, got WhenApplies=%q", skills[0].WhenApplies)
	}
}

func TestSkillLoader_LoadAll_NoSkillsDir(t *testing.T) {
	dir := t.TempDir()
	loader := &SkillLoader{ProjectDir: ".agentd/skills/", GlobalDir: filepath.Join(dir, "nonexistent")}
	skills, err := loader.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("got %d skills, want 0 for missing directory", len(skills))
	}
}

func TestSkillLoader_LoadAll_SkipsNonMD(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".agentd", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "readme.txt"), []byte("not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "notes.yaml"), []byte("key: val"), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := &SkillLoader{ProjectDir: ".agentd/skills/"}
	skills, err := loader.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("got %d skills, want 0 (non-.md files skipped)", len(skills))
	}
}

// ---------------------------------------------------------------------------
// SkillRouter
// ---------------------------------------------------------------------------

func TestSkillRouter_Match_MigrationTask(t *testing.T) {
	skills := []*Skill{
		{Name: "Migrations", WhenApplies: "database migrations, schema changes, alembic, flyway"},
		{Name: "Testing", WhenApplies: "unit tests, integration tests, test coverage"},
		{Name: "Deployment", WhenApplies: "deploy, release, CI/CD pipeline, staging"},
	}
	router := &SkillRouter{TopK: 3, Threshold: 0.0}
	matched := router.Match("add a database migration for the users table", skills)

	if len(matched) == 0 {
		t.Fatal("expected at least one match")
	}
	if matched[0].Name != "Migrations" {
		t.Fatalf("top match = %q, want %q", matched[0].Name, "Migrations")
	}
}

func TestSkillRouter_Match_BelowThreshold(t *testing.T) {
	skills := []*Skill{
		{Name: "Migrations", WhenApplies: "database migrations, schema changes"},
	}
	router := &SkillRouter{TopK: 3, Threshold: 0.99}
	matched := router.Match("completely unrelated task about painting", skills)

	if len(matched) != 0 {
		t.Fatalf("expected 0 matches (all below threshold), got %d", len(matched))
	}
}

func TestSkillRouter_Match_TopK(t *testing.T) {
	skills := []*Skill{
		{Name: "A", WhenApplies: "testing unit tests coverage"},
		{Name: "B", WhenApplies: "testing integration tests e2e"},
		{Name: "C", WhenApplies: "testing performance benchmarks load"},
		{Name: "D", WhenApplies: "testing security vulnerability scanning"},
	}
	router := &SkillRouter{TopK: 2, Threshold: 0.0}
	matched := router.Match("write tests for the authentication module", skills)

	if len(matched) > 2 {
		t.Fatalf("expected at most 2 matches (topK=2), got %d", len(matched))
	}
}

func TestSkillRouter_Match_EmptyTask(t *testing.T) {
	skills := []*Skill{
		{Name: "A", WhenApplies: "anything"},
	}
	router := &SkillRouter{TopK: 3, Threshold: 0.0}
	matched := router.Match("", skills)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches for empty task, got %d", len(matched))
	}
}

func TestSkillRouter_Match_NoSkills(t *testing.T) {
	router := &SkillRouter{TopK: 3, Threshold: 0.0}
	matched := router.Match("do something", nil)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches for nil skills, got %d", len(matched))
	}
}

func TestSkillRouter_Match_DefaultTopK(t *testing.T) {
	router := &SkillRouter{} // TopK=0 → defaults to 3
	skills := []*Skill{
		{Name: "A", WhenApplies: "test"},
		{Name: "B", WhenApplies: "test"},
		{Name: "C", WhenApplies: "test"},
		{Name: "D", WhenApplies: "test"},
	}
	matched := router.Match("test", skills)
	if len(matched) > 3 {
		t.Fatalf("expected at most 3 matches (default topK), got %d", len(matched))
	}
}

// ---------------------------------------------------------------------------
// TF-IDF internals
// ---------------------------------------------------------------------------

func TestTokenize(t *testing.T) {
	tf := tokenize("Hello World, hello again!")
	if tf["hello"] != 2 {
		t.Fatalf("expected hello=2, got %d", tf["hello"])
	}
	if tf["world"] != 1 {
		t.Fatalf("expected world=1, got %d", tf["world"])
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	a := map[string]float64{"foo": 1.0, "bar": 2.0}
	sim := cosineSimilarity(a, a)
	if sim < 0.999 {
		t.Fatalf("identical vectors should have similarity ~1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := map[string]float64{"foo": 1.0}
	b := map[string]float64{"bar": 1.0}
	sim := cosineSimilarity(a, b)
	if sim != 0 {
		t.Fatalf("orthogonal vectors should have similarity 0, got %f", sim)
	}
}

// ---------------------------------------------------------------------------
// FormatSkillBlock
// ---------------------------------------------------------------------------

func TestFormatSkillBlock(t *testing.T) {
	sk := &Skill{
		Name:        "Deploy",
		WhenApplies: "deploy, release",
		Procedure:   "1. Build\n2. Push",
	}
	block := FormatSkillBlock(sk)
	if !strings.Contains(block, "=== Skill: Deploy ===") {
		t.Fatalf("block missing skill header: %q", block)
	}
	if !strings.Contains(block, "deploy, release") {
		t.Fatalf("block missing WhenApplies: %q", block)
	}
	if !strings.Contains(block, "1. Build") {
		t.Fatalf("block missing Procedure: %q", block)
	}
}

func TestFormatSkillBlock_Nil(t *testing.T) {
	block := FormatSkillBlock(nil)
	if block != "" {
		t.Fatalf("expected empty block for nil skill, got %q", block)
	}
}

// ---------------------------------------------------------------------------
// SystemPromptBuilder.AddSkillBlock integration
// ---------------------------------------------------------------------------

func TestSystemPromptBuilder_AddSkillBlock(t *testing.T) {
	prompt := NewSystemPromptBuilder().
		WithGlobal("global instructions").
		AddSkillBlock("=== Skill: Deploy ===\nProcedure: push").
		WithTask("fix the deploy").
		Build()

	if !strings.Contains(prompt, "MATCHED SKILLS (contextual guidance):") {
		t.Fatal("prompt missing MATCHED SKILLS header")
	}
	if !strings.Contains(prompt, "=== Skill: Deploy ===") {
		t.Fatal("prompt missing skill block content")
	}

	// Verify ordering: skills appear after global but before task.
	globalIdx := strings.Index(prompt, "global instructions")
	skillIdx := strings.Index(prompt, "MATCHED SKILLS")
	taskIdx := strings.Index(prompt, "fix the deploy")
	if globalIdx >= skillIdx {
		t.Fatal("skills should appear after global instructions")
	}
	if skillIdx >= taskIdx {
		t.Fatal("skills should appear before task instructions")
	}
}

func TestSystemPromptBuilder_NoSkillBlocks(t *testing.T) {
	prompt := NewSystemPromptBuilder().
		WithGlobal("global").
		Build()
	if strings.Contains(prompt, "MATCHED SKILLS") {
		t.Fatal("prompt should not contain MATCHED SKILLS when none added")
	}
}
