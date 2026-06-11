package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBaselineMissingFile(t *testing.T) {
	t.Parallel()

	got, err := readBaseline(filepath.Join(t.TempDir(), "missing.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("readBaseline missing file = %d entries, want 0", len(got))
	}
}

func TestReadBaseline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.txt")
	if err := os.WriteFile(path, []byte("# comment\n\na.go f\nb.go (*T).g\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["a.go f"]; !ok {
		t.Fatal("missing first baseline entry")
	}
	if _, ok := got["b.go (*T).g"]; !ok {
		t.Fatal("missing second baseline entry")
	}
}

func TestReadBaselineDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "baseline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.txt"), []byte("b.go (*T).g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-001.txt"), []byte("a.go f\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readBaseline(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["a.go f"]; !ok {
		t.Fatal("missing first baseline entry")
	}
	if _, ok := got["b.go (*T).g"]; !ok {
		t.Fatal("missing second baseline entry")
	}
}

func TestNewViolations(t *testing.T) {
	t.Parallel()

	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
		{name: "g", file: "b.go", line: 2, lines: 2},
	}
	accepted := map[string]struct{}{
		violationKey(violations[0]): {},
	}

	got := newViolations(violations, accepted)
	if len(got) != 1 {
		t.Fatalf("newViolations returned %d entries, want 1", len(got))
	}
	if got[0].name != "g" {
		t.Fatalf("newViolations returned %q, want g", got[0].name)
	}
}

func TestWriteBaselineBackup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.txt")
	original := "a.go f\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	violations := []violation{{name: "g", file: "b.go", line: 2, lines: 2}}
	if err := writeBaseline(path, violations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("backup = %q, want %q", got, original)
	}
}

func TestWriteBaseline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.txt")
	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
		{name: "g", file: "b.go", line: 2, lines: 2},
	}

	if err := writeBaseline(path, violations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "a.go f\nb.go g\n"
	if string(got) != want {
		t.Fatalf("baseline = %q, want %q", got, want)
	}
}

func TestWriteBaselineDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline")
	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
		{name: "g", file: "b.go", line: 2, lines: 2},
	}

	if err := writeBaseline(path, violations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(path, "part-001.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := "a.go f\nb.go g\n"
	if string(got) != want {
		t.Fatalf("baseline = %q, want %q", got, want)
	}
}
