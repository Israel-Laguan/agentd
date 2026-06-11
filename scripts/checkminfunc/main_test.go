package main

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"
)

func TestTypeName(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{name: "ident", expr: &ast.Ident{Name: "MyType"}, want: "MyType"},
		{name: "selector", expr: &ast.SelectorExpr{X: &ast.Ident{Name: "pkg"}, Sel: &ast.Ident{Name: "Type"}}, want: "pkg.Type"},
		{name: "star", expr: &ast.StarExpr{X: &ast.Ident{Name: "MyType"}}, want: "*MyType"},
		{name: "index single", expr: &ast.IndexExpr{X: &ast.Ident{Name: "Map"}, Index: &ast.Ident{Name: "string"}}, want: "Map[string]"},
		{name: "index list", expr: &ast.IndexListExpr{X: &ast.Ident{Name: "Map"}, Indices: []ast.Expr{&ast.Ident{Name: "string"}, &ast.Ident{Name: "int"}}}, want: "Map[string, int]"},
		{name: "nested index", expr: &ast.IndexExpr{X: &ast.IndexListExpr{X: &ast.Ident{Name: "Either"}, Indices: []ast.Expr{&ast.Ident{Name: "string"}, &ast.Ident{Name: "error"}}}, Index: &ast.Ident{Name: "bool"}}, want: "Either[string, error][bool]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := typeName(tt.expr)
			if got != tt.want {
				t.Fatalf("typeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

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

func TestWriteBaselineDirectoryCleansStaleFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline")
	staleFile := filepath.Join(path, "part-001.txt")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleFile, []byte("old.go oldFunc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
	}

	if err := writeBaseline(path, violations); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(staleFile)
	if err != nil {
		t.Fatalf("new part-001.txt should exist: %v", err)
	}
	want := "a.go f\n"
	if string(got) != want {
		t.Fatalf("baseline = %q, want %q", got, want)
	}
}
