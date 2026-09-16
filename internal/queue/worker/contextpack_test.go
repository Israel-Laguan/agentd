package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestContextPack_Validate_Valid(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1,
		TaskID:  "t1",
		Summary: "gather context",
		Paths:   []string{"src/main.go"},
		Budget:  ContextBudget{MaxPaths: 40, MaxChars: 48000, PathCount: 1, CharCount: 14},
	}
	if err := cp.Validate(); err != nil {
		t.Fatalf("expected valid pack: %v", err)
	}
}

func TestContextPack_Validate_BadVersion(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{Version: 2, TaskID: "t1", Summary: "s", Paths: []string{"x"}}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected version error, got: %v", err)
	}
}

func TestContextPack_Validate_MissingTaskID(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{Version: 1, Summary: "s", Paths: []string{"x"}}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("expected task_id error, got: %v", err)
	}
}

func TestContextPack_Validate_MissingSummary(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{Version: 1, TaskID: "t1", Paths: []string{"x"}}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("expected summary error, got: %v", err)
	}
}

func TestContextPack_Validate_EmptyPaths(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{Version: 1, TaskID: "t1", Summary: "s"}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "at least one path") {
		t.Fatalf("expected paths error, got: %v", err)
	}
}

func TestContextPack_Validate_EmptyPathString(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{Version: 1, TaskID: "t1", Summary: "s", Paths: []string{""}}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty path error, got: %v", err)
	}
}

func TestContextPack_Validate_DuplicatePaths(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{Version: 1, TaskID: "t1", Summary: "s", Paths: []string{"a.go", "a.go"}}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate path") {
		t.Fatalf("expected duplicate path error, got: %v", err)
	}
}

func TestContextPack_Validate_ExcerptMissingPath(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "s",
		Paths:    []string{"a.go"},
		Excerpts: []ContextExcerpt{{Path: "", Note: "why"}},
	}
	if err := cp.Validate(); err == nil || !strings.Contains(err.Error(), "excerpt 0: path") {
		t.Fatalf("expected excerpt path error, got: %v", err)
	}
}

func TestContextPack_CharCount(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Summary:     "hello",
		Excerpts:    []ContextExcerpt{{Path: "a", Note: "world"}},
		Constraints: []string{"must not x"},
		Unknowns:    []string{"y?"},
	}
	got := cp.CharCount()
	// 5 + 5 + 10 + 2 = 22
	if got != 22 {
		t.Fatalf("CharCount() = %d, want 22", got)
	}
}

func TestContextPack_CharCount_IncludesCommandsRun(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Summary:     "hi",
		CommandsRun: []CommandRun{{Cmd: "echo hello", Outcome: "ok"}},
	}
	// hi=2 + echo hello=10 + ok=2 =14
	got := cp.CharCount()
	if got != 14 {
		t.Fatalf("CharCount() = %d, want 14", got)
	}
}

func TestContextPack_EnforceBudget_PathsExceeded(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "s",
		Paths: []string{"a", "b", "c", "d", "e"},
	}
	if _, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 3, MaxChars: 100000}); err != nil {
		t.Fatalf("EnforceBudget error = %v", err)
	}
	if len(cp.Paths) != 3 {
		t.Fatalf("Paths len = %d, want 3", len(cp.Paths))
	}
	if cp.Budget.MaxPaths != 3 {
		t.Fatalf("Budget.MaxPaths = %d, want 3", cp.Budget.MaxPaths)
	}
	if cp.Budget.PathCount != 3 {
		t.Fatalf("Budget.PathCount = %d, want 3", cp.Budget.PathCount)
	}
}

func TestContextPack_EnforceBudget_TextOverflow(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "ok",
		Paths:    []string{"a"},
		Unknowns: []string{strings.Repeat("x", 200)},
	}
	if _, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 40, MaxChars: 100}); err != nil {
		t.Fatalf("EnforceBudget error = %v", err)
	}
	if len(cp.Unknowns) != 0 {
		t.Fatalf("Unknowns should be trimmed, got %d entries", len(cp.Unknowns))
	}
}

func TestContextPack_EnforceBudget_NoopWhenUnderBudget(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "short",
		Paths: []string{"a.go"},
	}
	truncated, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 40, MaxChars: 48000})
	if err != nil {
		t.Fatalf("EnforceBudget error = %v", err)
	}
	if truncated {
		t.Fatal("should not truncate when under budget")
	}
	if cp.Budget.CharCount != cp.CharCount() {
		t.Fatalf("Budget.CharCount = %d, want %d", cp.Budget.CharCount, cp.CharCount())
	}
}

func TestContextPack_EnforceBudget_DefaultsWhenZero(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "s",
		Paths: []string{"a"},
	}
	if _, err := cp.EnforceBudget(ContextPackConfig{}); err != nil {
		t.Fatalf("EnforceBudget error = %v", err)
	}
	if cp.Budget.MaxPaths != DefaultMaxContextPackPaths {
		t.Fatalf("Budget.MaxPaths = %d, want %d", cp.Budget.MaxPaths, DefaultMaxContextPackPaths)
	}
	if cp.Budget.MaxChars != DefaultMaxContextPackChars {
		t.Fatalf("Budget.MaxChars = %d, want %d", cp.Budget.MaxChars, DefaultMaxContextPackChars)
	}
}

func TestContextPack_EnforceBudget_OversizedSummary(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: strings.Repeat("x", 200),
		Paths: []string{"a"},
	}
	_, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 40, MaxChars: 100})
	if err == nil || !strings.Contains(err.Error(), "required content") {
		t.Fatalf("expected required content error for oversized summary, got %v", err)
	}
}

func TestContextPack_EnforceBudget_OversizedExcerpts(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "ok",
		Paths:    []string{"a"},
		Excerpts: []ContextExcerpt{{Path: "a.go", Note: strings.Repeat("n", 200), Span: strings.Repeat("s", 200)}},
	}
	_, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 40, MaxChars: 100})
	if err == nil || !strings.Contains(err.Error(), "required content") {
		t.Fatalf("expected required content error for oversized excerpts, got %v", err)
	}
}

func TestContextPack_EnforceBudget_OversizedCommandsRun(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "ok",
		Paths:       []string{"a"},
		CommandsRun: []CommandRun{{Cmd: strings.Repeat("c", 200), Outcome: strings.Repeat("o", 200)}},
	}
	_, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 40, MaxChars: 100})
	if err == nil || !strings.Contains(err.Error(), "required content") {
		t.Fatalf("expected required content error for oversized CommandsRun, got %v", err)
	}
}

func TestContextPack_EnforceBudget_TrimsOptionalKeepsRequired(t *testing.T) {
	t.Parallel()
	cp := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "ok",
		Paths:       []string{"a"},
		Constraints: []string{strings.Repeat("c", 50)},
		Unknowns:    []string{strings.Repeat("u", 50)},
		CommandsRun: []CommandRun{{Cmd: "ls", Outcome: "ok"}},
	}
	// required = 2 + 2+2 =6, optional 100, budget 50 => optional trimmed but required stays
	truncated, err := cp.EnforceBudget(ContextPackConfig{MaxPaths: 40, MaxChars: 50})
	if err != nil {
		t.Fatalf("EnforceBudget error = %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(cp.Unknowns) != 0 || len(cp.Constraints) != 0 {
		t.Fatalf("expected optional trimmed, got unknowns=%d constraints=%d", len(cp.Unknowns), len(cp.Constraints))
	}
	if cp.CharCount() > 50 {
		t.Fatalf("CharCount %d still over budget 50", cp.CharCount())
	}
}

func TestWriteContextPack_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cp := &ContextPack{
		Version:      1,
		TaskID:       "t1",
		ParentTaskID: "p1",
		CreatedAt:    time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Summary:      "gathered context",
		Paths:        []string{"src/main.go", "config.yaml"},
		Excerpts:     []ContextExcerpt{{Path: "src/main.go", Note: "entry point"}},
		CommandsRun:  []CommandRun{{Cmd: "ls", Outcome: "ok"}},
		Constraints:  []string{"do not modify config"},
		Unknowns:     []string{"is there a test runner?"},
		Budget:       ContextBudget{MaxPaths: 40, MaxChars: 48000, PathCount: 2},
	}
	cp.Budget.CharCount = cp.CharCount()
	cp.Budget.PathCount = len(cp.Paths)
	if err := WriteContextPack(dir, cp); err != nil {
		t.Fatalf("WriteContextPack: %v", err)
	}
	path := filepath.Join(dir, PackFilePath(1))
	got, err := ReadContextPack(path)
	if err != nil {
		t.Fatalf("ReadContextPack: %v", err)
	}
	if got.TaskID != "t1" {
		t.Fatalf("TaskID = %q, want t1", got.TaskID)
	}
	if got.ParentTaskID != "p1" {
		t.Fatalf("ParentTaskID = %q, want p1", got.ParentTaskID)
	}
	if len(got.Paths) != 2 {
		t.Fatalf("Paths len = %d, want 2", len(got.Paths))
	}
	if len(got.Excerpts) != 1 {
		t.Fatalf("Excerpts len = %d, want 1", len(got.Excerpts))
	}
	if got.Excerpts[0].Note != "entry point" {
		t.Fatalf("Excerpt Note = %q", got.Excerpts[0].Note)
	}
	if len(got.CommandsRun) != 1 {
		t.Fatalf("CommandsRun len = %d, want 1", len(got.CommandsRun))
	}
	if got.Constraints[0] != "do not modify config" {
		t.Fatalf("Constraints[0] = %q", got.Constraints[0])
	}
}

func TestWriteContextPack_InvalidPack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cp := &ContextPack{Version: 1} // missing task_id
	if err := WriteContextPack(dir, cp); err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestReadContextPack_BadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	_, err := ReadContextPack(path)
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got: %v", err)
	}
}

func TestReadContextPack_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadContextPack("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadContextPack_BackfillsMissingBudgetCounters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Legacy v1 pack: budget counters were not written before they became
	// mandatory (matches the v1 sketch in docs/tiered-execution.md).
	legacy := `{
  "version": 1,
  "task_id": "legacy-task",
  "created_at": "2026-01-15T10:00:00Z",
  "summary": "legacy pack",
  "paths": ["src/main.go", "config.yaml"],
  "budget": {"max_paths": 40, "max_chars": 48000}
}`
	path := filepath.Join(dir, PackFilePath(1))
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	got, err := ReadContextPack(path)
	if err != nil {
		t.Fatalf("ReadContextPack: %v", err)
	}
	if got.Budget.PathCount != len(got.Paths) {
		t.Fatalf("Budget.PathCount = %d, want %d", got.Budget.PathCount, len(got.Paths))
	}
	if got.Budget.CharCount != got.CharCount() {
		t.Fatalf("Budget.CharCount = %d, want %d", got.Budget.CharCount, got.CharCount())
	}

	// A pack that recorded only one of the two counters must keep the value it
	// has and backfill only the missing one.
	partial := `{
  "version": 1,
  "task_id": "partial-task",
  "created_at": "2026-01-15T10:00:00Z",
  "summary": "partial pack",
  "paths": ["src/main.go", "config.yaml"],
  "budget": {"max_paths": 40, "max_chars": 48000, "path_count": 2}
}`
	partialPath := filepath.Join(dir, "partial.json")
	if err := os.WriteFile(partialPath, []byte(partial), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	got, err = ReadContextPack(partialPath)
	if err != nil {
		t.Fatalf("ReadContextPack(partial): %v", err)
	}
	if got.Budget.PathCount != 2 {
		t.Fatalf("Budget.PathCount = %d, want 2 (explicit counter must be untouched)", got.Budget.PathCount)
	}
	if got.Budget.CharCount != got.CharCount() {
		t.Fatalf("Budget.CharCount = %d, want %d", got.Budget.CharCount, got.CharCount())
	}
}

func TestReadContextPack_ExplicitZeroCountersFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := map[string]string{
		"explicit path_count zero": `{
  "version": 1,
  "task_id": "zero-path",
  "created_at": "2026-01-15T10:00:00Z",
  "summary": "zero path",
  "paths": ["a.go", "b.go"],
  "budget": {"max_paths": 40, "max_chars": 48000, "path_count": 0, "char_count": 9}
}`,
		"explicit char_count zero": `{
  "version": 1,
  "task_id": "zero-char",
  "created_at": "2026-01-15T10:00:00Z",
  "summary": "zero char",
  "paths": ["a.go"],
  "budget": {"max_paths": 40, "max_chars": 48000, "path_count": 1, "char_count": 0}
}`,
	}
	for name, doc := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".json")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatalf("os.WriteFile: %v", err)
		}
		if _, err := ReadContextPack(path); err == nil {
			t.Fatalf("%s: expected explicit zero counter to fail validation", name)
		}
	}
}

func TestContextPack_Validate_MismatchedCountersStillFail(t *testing.T) {
	t.Parallel()
	// BackfillBudgetCounters only fills zero-valued counters; an explicit
	// counter that disagrees with the serialized content must still fail.
	pathMismatch := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "summary",
		Paths:  []string{"a.go"},
		Budget: ContextBudget{MaxPaths: 40, MaxChars: 48000, PathCount: 7, CharCount: 7},
	}
	if err := pathMismatch.Validate(); err == nil || !strings.Contains(err.Error(), "path_count") {
		t.Fatalf("expected path_count mismatch error, got: %v", err)
	}
	// "summary" is 7 runes, so 8 is a genuine mismatch.
	charMismatch := &ContextPack{
		Version: 1, TaskID: "t1", Summary: "summary",
		Paths:  []string{"a.go"},
		Budget: ContextBudget{MaxPaths: 40, MaxChars: 48000, PathCount: 1, CharCount: 8},
	}
	if err := charMismatch.Validate(); err == nil || !strings.Contains(err.Error(), "char_count") {
		t.Fatalf("expected char_count mismatch error, got: %v", err)
	}
}

func TestNewContextPack(t *testing.T) {
	t.Parallel()
	cp := NewContextPack("t1", "p1", "gathered info", []string{"a.go", "b.go"}, ContextPackConfig{MaxPaths: 40, MaxChars: 48000})
	if cp.Version != 1 {
		t.Fatalf("Version = %d, want 1", cp.Version)
	}
	if cp.TaskID != "t1" {
		t.Fatalf("TaskID = %q, want t1", cp.TaskID)
	}
	if cp.ParentTaskID != "p1" {
		t.Fatalf("ParentTaskID = %q, want p1", cp.ParentTaskID)
	}
	if cp.Summary != "gathered info" {
		t.Fatalf("Summary = %q", cp.Summary)
	}
	if len(cp.Paths) != 2 {
		t.Fatalf("Paths len = %d, want 2", len(cp.Paths))
	}
	if cp.Budget.PathCount != 2 {
		t.Fatalf("Budget.PathCount = %d, want 2", cp.Budget.PathCount)
	}
	if cp.Budget.CharCount != utf8.RuneCountInString("gathered info") {
		t.Fatalf("Budget.CharCount = %d, want %d", cp.Budget.CharCount, utf8.RuneCountInString("gathered info"))
	}
	if cp.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should not be zero")
	}
}

func TestContextPack_JSON_RoundTrip_RawBytes(t *testing.T) {
	t.Parallel()
	cp := NewContextPack("t42", "", "summary", []string{"x.go"}, ContextPackConfig{MaxPaths: 40, MaxChars: 48000})
	cp.Excerpts = append(cp.Excerpts, ContextExcerpt{Path: "x.go", Note: "entry"})
	cp.Constraints = []string{"no rewrite"}
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ContextPack
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskID != "t42" {
		t.Fatalf("TaskID = %q", got.TaskID)
	}
	if len(got.Excerpts) != 1 {
		t.Fatalf("Excerpts len = %d", len(got.Excerpts))
	}
}

func TestPackFilePath(t *testing.T) {
	t.Parallel()
	got := PackFilePath(1)
	if got != "context_pack.v1.json" {
		t.Fatalf("PackFilePath(1) = %q, want context_pack.v1.json", got)
	}
}
