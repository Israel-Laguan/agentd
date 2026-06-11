package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBaseline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.txt")
	if err := os.WriteFile(path, []byte("# comment\n\na.go:1 f\nb.go:2 (*T).g\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["a.go:1 f"]; !ok {
		t.Fatal("missing first baseline entry")
	}
	if _, ok := got["b.go:2 (*T).g"]; !ok {
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
	want := "a.go:1 f\nb.go:2 g\n"
	if string(got) != want {
		t.Fatalf("baseline = %q, want %q", got, want)
	}
}
