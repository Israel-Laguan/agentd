package worker

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// parseSubagentMD
// ---------------------------------------------------------------------------

func TestParseSubagentMD_FullDocument(t *testing.T) {
	content := `# Subagent: Code Reviewer

## Purpose

Review code changes for style violations and potential bugs.

## Allowed Tools

- read
- bash

## Forbidden Tools

- write

## Max Iterations

15

## Context Budget

12000

## Output Schema

JSON with fields: issues (array), summary (string)

## Termination Criteria

Stop after reviewing all modified files or when iteration limit is reached.
`
	def := parseSubagentMD(content, "/tmp/reviewer.md")
	if def.Name != "Code Reviewer" {
		t.Fatalf("Name = %q, want %q", def.Name, "Code Reviewer")
	}
	if def.Purpose == "" {
		t.Fatal("Purpose should not be empty")
	}
	if len(def.AllowedTools) != 2 {
		t.Fatalf("AllowedTools = %v, want [read, bash]", def.AllowedTools)
	}
	if def.AllowedTools[0] != "read" || def.AllowedTools[1] != "bash" {
		t.Fatalf("AllowedTools = %v, want [read, bash]", def.AllowedTools)
	}
	if len(def.ForbiddenTools) != 1 || def.ForbiddenTools[0] != "write" {
		t.Fatalf("ForbiddenTools = %v, want [write]", def.ForbiddenTools)
	}
	if def.MaxIterations != 15 {
		t.Fatalf("MaxIterations = %d, want 15", def.MaxIterations)
	}
	if def.ContextBudget != 12000 {
		t.Fatalf("ContextBudget = %d, want 12000", def.ContextBudget)
	}
	if def.OutputSchema == "" {
		t.Fatal("OutputSchema should not be empty")
	}
	if def.TerminationCriteria == "" {
		t.Fatal("TerminationCriteria should not be empty")
	}
	if def.SourcePath != "/tmp/reviewer.md" {
		t.Fatalf("SourcePath = %q, want %q", def.SourcePath, "/tmp/reviewer.md")
	}
}

func TestParseSubagentMD_Empty(t *testing.T) {
	def := parseSubagentMD("", "/tmp/empty.md")
	if def.Name != "" {
		t.Fatalf("Name = %q, want empty", def.Name)
	}
}

func TestParseSubagentMD_MinimalDefinition(t *testing.T) {
	content := `# Subagent: Helper

## Purpose

Help with stuff.
`
	def := parseSubagentMD(content, "/tmp/helper.md")
	if def.Name != "Helper" {
		t.Fatalf("Name = %q, want %q", def.Name, "Helper")
	}
	if def.Purpose != "Help with stuff." {
		t.Fatalf("Purpose = %q", def.Purpose)
	}
	if len(def.AllowedTools) != 0 {
		t.Fatalf("AllowedTools should be empty, got %v", def.AllowedTools)
	}
	if def.MaxIterations != 0 {
		t.Fatalf("MaxIterations = %d, want 0 (default)", def.MaxIterations)
	}
}

// ---------------------------------------------------------------------------
// extractSubagentName
// ---------------------------------------------------------------------------

func TestExtractSubagentName_WithPrefix(t *testing.T) {
	got := extractSubagentName("# Subagent: Test Agent\n\nsome content")
	if got != "Test Agent" {
		t.Fatalf("got %q, want %q", got, "Test Agent")
	}
}

func TestExtractSubagentName_WithoutPrefix(t *testing.T) {
	got := extractSubagentName("# My Agent\n\nsome content")
	if got != "My Agent" {
		t.Fatalf("got %q, want %q", got, "My Agent")
	}
}

func TestExtractSubagentName_NoHeading(t *testing.T) {
	got := extractSubagentName("no heading here\njust content")
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// parseToolList
// ---------------------------------------------------------------------------

func TestParseToolList(t *testing.T) {
	input := "- bash\n- read\n- write\n"
	got := parseToolList(input)
	if len(got) != 3 {
		t.Fatalf("expected 3 tools, got %d: %v", len(got), got)
	}
	if got[0] != "bash" || got[1] != "read" || got[2] != "write" {
		t.Fatalf("unexpected tools: %v", got)
	}
}

func TestParseToolList_Asterisk(t *testing.T) {
	input := "* tool_a\n* tool_b\n"
	got := parseToolList(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(got), got)
	}
}

func TestParseToolList_Empty(t *testing.T) {
	got := parseToolList("")
	if len(got) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// SubagentLoader
// ---------------------------------------------------------------------------

func TestSubagentLoader_LoadAll(t *testing.T) {
	dir := t.TempDir()
	subagentDir := filepath.Join(dir, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `# Subagent: Test Runner

## Purpose

Run tests and report results.

## Allowed Tools

- bash
- read
`
	if err := os.WriteFile(filepath.Join(subagentDir, "test-runner.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &SubagentLoader{}
	defs, err := loader.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Name != "Test Runner" {
		t.Fatalf("Name = %q, want %q", defs[0].Name, "Test Runner")
	}
	if len(defs[0].AllowedTools) != 2 {
		t.Fatalf("AllowedTools = %v, want [bash, read]", defs[0].AllowedTools)
	}
}

func TestSubagentLoader_LoadAll_NoDirectory(t *testing.T) {
	loader := &SubagentLoader{}
	defs, err := loader.LoadAll(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs != nil {
		t.Fatalf("expected nil, got %v", defs)
	}
}

func TestSubagentLoader_LoadAll_EmptyWorkspace(t *testing.T) {
	loader := &SubagentLoader{}
	defs, err := loader.LoadAll("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs != nil {
		t.Fatalf("expected nil, got %v", defs)
	}
}

func TestSubagentLoader_LoadByName(t *testing.T) {
	dir := t.TempDir()
	subagentDir := filepath.Join(dir, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `# Subagent: Linter

## Purpose

Run linting tools.
`
	if err := os.WriteFile(filepath.Join(subagentDir, "linter.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &SubagentLoader{}
	def, err := loader.LoadByName(dir, "Linter")
	if err != nil {
		t.Fatalf("LoadByName failed: %v", err)
	}
	if def.Name != "Linter" {
		t.Fatalf("Name = %q, want %q", def.Name, "Linter")
	}
}

func TestSubagentLoader_LoadByName_NotFound(t *testing.T) {
	dir := t.TempDir()
	subagentDir := filepath.Join(dir, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatal(err)
	}

	loader := &SubagentLoader{}
	_, err := loader.LoadByName(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent subagent")
	}
}

func TestSubagentLoader_SkipsNonMDFiles(t *testing.T) {
	dir := t.TempDir()
	subagentDir := filepath.Join(dir, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a .txt file that should be ignored
	if err := os.WriteFile(filepath.Join(subagentDir, "readme.txt"), []byte("not a definition"), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &SubagentLoader{}
	defs, err := loader.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 definitions, got %d", len(defs))
	}
}

func TestSubagentLoader_SkipsEmptyNameFiles(t *testing.T) {
	dir := t.TempDir()
	subagentDir := filepath.Join(dir, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a .md file with no heading
	if err := os.WriteFile(filepath.Join(subagentDir, "empty.md"), []byte("no heading here"), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &SubagentLoader{}
	defs, err := loader.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 definitions (no name), got %d", len(defs))
	}
}
