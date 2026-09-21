package main

import (
	"go/ast"
	"go/token"
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
		{name: "slice", expr: &ast.ArrayType{Elt: &ast.Ident{Name: "int"}}, want: "[]int"},
		{name: "array", expr: &ast.ArrayType{Len: &ast.BasicLit{Value: "10"}, Elt: &ast.Ident{Name: "int"}}, want: "[10]int"},
		{name: "map", expr: &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.Ident{Name: "int"}}, want: "map[string]int"},
		{name: "func", expr: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{{Name: "x"}}, Type: &ast.Ident{Name: "int"}}}}, Results: &ast.FieldList{List: []*ast.Field{{Type: &ast.Ident{Name: "error"}}}}}, want: "func(x int) error"},
		{name: "func no params", expr: &ast.FuncType{Results: &ast.FieldList{List: []*ast.Field{{Type: &ast.Ident{Name: "error"}}}}}, want: "func() error"},
		{name: "struct", expr: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{{Name: "Name"}}, Type: &ast.Ident{Name: "string"}}}}}, want: "struct{Name string}"},
		{name: "empty struct", expr: &ast.StructType{}, want: "struct{}"},
		{name: "interface", expr: &ast.InterfaceType{Methods: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{{Name: "Close"}}, Type: &ast.FuncType{}}}}}, want: "interface{Close()}"},
		{name: "empty interface", expr: &ast.InterfaceType{}, want: "interface{}"},
		{name: "chan", expr: &ast.ChanType{Dir: ast.RECV, Value: &ast.Ident{Name: "int"}}, want: "<-chan int"},
		{name: "paren", expr: &ast.ParenExpr{X: &ast.Ident{Name: "int"}}, want: "(int)"},
		{name: "basic lit", expr: &ast.BasicLit{Value: "10"}, want: "10"},
		{name: "ellipsis", expr: &ast.Ellipsis{Elt: &ast.Ident{Name: "int"}}, want: "...int"},
		{name: "type set", expr: &ast.BinaryExpr{Op: token.OR, X: &ast.UnaryExpr{Op: token.TILDE, X: &ast.Ident{Name: "int"}}, Y: &ast.Ident{Name: "string"}}, want: "~int | string"},
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

func TestValidateBaselineWritePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := validateBaselineWritePath(filepath.Join("scripts", "checkminfunc", "baseline")); err != nil {
		t.Fatal(err)
	}
	if err := validateBaselineWritePath(filepath.Join(root, "scripts", "baseline")); err != nil {
		t.Fatal(err)
	}
	if err := validateBaselineWritePath(filepath.Join(t.TempDir(), "baseline")); err == nil {
		t.Fatal("expected outside-path validation error")
	}

	linkDir := filepath.Join(root, "linkdir")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside-baseline")
	if err := os.Symlink(target, filepath.Join(linkDir, "baseline")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := validateBaselineWritePath(filepath.Join(linkDir, "baseline")); err == nil {
		t.Fatal("expected symlink escape validation error")
	}
}

func TestCheckFileCountsSignificantLinesOnly(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("a.go", []byte(significantLinesTestSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := checkFile("a.go", 3)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]int, len(violations))
	for _, v := range violations {
		got[v.name] = v.lines
	}
	want := map[string]int{
		"padded": 2,
		"trick":  2,
		"single": 1,
		"empty":  0,
	}
	if len(got) != len(want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
	for name, lines := range want {
		if got[name] != lines {
			t.Fatalf("violation %q = %d lines, want %d (all = %v)", name, got[name], lines, got)
		}
	}
}

const significantLinesTestSrc = `package p

import "context"

type ctxKey struct{}

// padded looks long but has only two lines of code.
func padded(ctx context.Context) string {
	// comment one
	// comment two

	/* block
	   comment */

	id, _ := ctx.Value(ctxKey{}).(string) // trailing comment
	return id
}

// real has three lines of code.
func real() int {
	a := 1
	b := 2
	return a + b
}

// trick contains a string that looks like a comment.
func trick() string {
	s := "// not a comment"
	return s
}

// single is a one-line body.
func single() int { return 1 }

// empty has no code at all.
func empty() {
	// nothing here
}
`

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

func TestNewViolationsEmptyAcceptedReturnsCopy(t *testing.T) {
	t.Parallel()

	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
	}

	got := newViolations(violations, nil)
	if len(got) != 1 {
		t.Fatalf("newViolations returned %d entries, want 1", len(got))
	}
	got[0].name = "mutated"

	if violations[0].name != "f" {
		t.Fatalf("newViolations returned original slice; original violation became %q", violations[0].name)
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
	backupPath, err := writeBaseline(path, violations, 3)
	if err != nil {
		t.Fatal(err)
	}
	if backupPath == "" {
		t.Fatal("expected backup path to be returned")
	}
	got, err := os.ReadFile(backupPath)
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

	if _, err := writeBaseline(path, violations, 3); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# minfunc baseline - accepted functions with < 3 significant lines.\n# Generated by: go run ./scripts/checkminfunc --update-baseline\n# Do not edit manually. Run `make minfunc-accept` (or the go command) to update.\n# Format: <path> <FuncName> or <path> (Type).MethodName\n#\na.go f\nb.go g\n"
	if string(got) != want {
		t.Fatalf("baseline = %q, want %q", got, want)
	}
}

func TestWriteBaselineExtensionlessFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "README")
	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
	}

	if _, err := writeBaseline(path, violations, 3); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatalf("baseline path %q was created as a directory", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# minfunc baseline - accepted functions with < 3 significant lines.\n# Generated by: go run ./scripts/checkminfunc --update-baseline\n# Do not edit manually. Run `make minfunc-accept` (or the go command) to update.\n# Format: <path> <FuncName> or <path> (Type).MethodName\n#\na.go f\n"
	if string(got) != want {
		t.Fatalf("baseline = %q, want single extensionless file", got)
	}
}

func TestWriteBaselineDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline") + string(os.PathSeparator)
	violations := []violation{
		{name: "f", file: "a.go", line: 1, lines: 1},
		{name: "g", file: "b.go", line: 2, lines: 2},
	}

	if _, err := writeBaseline(path, violations, 3); err != nil {
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

	if _, err := writeBaseline(path, violations, 3); err != nil {
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

func TestWriteBaselineBackupDoesNotFollowSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.txt")
	original := "a.go f\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "sensitive.txt")
	if err := os.WriteFile(target, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	bakPath := path + ".bak"
	if err := os.Symlink(target, bakPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	violations := []violation{{name: "g", file: "b.go", line: 2, lines: 2}}
	backupPath, err := writeBaseline(path, violations, 3)
	if err != nil {
		t.Fatal(err)
	}
	if backupPath != bakPath {
		t.Fatalf("backupPath = %q, want %q", backupPath, bakPath)
	}

	// .bak must be regular file (symlink entry replaced by rename, not followed)
	info, err := os.Lstat(bakPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal(".bak must not be a symlink after backup")
	}

	// target must be untouched
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "SECRET" {
		t.Fatalf("target overwritten to %q", got)
	}

	// backup contains the original data
	got, err = os.ReadFile(bakPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("backup = %q, want %q", got, original)
	}
}
