package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  []byte
		want int
	}{
		{nil, 0},
		{[]byte{}, 0},
		{[]byte("a"), 1},
		{[]byte("a\n"), 1},
		{[]byte("a\nb"), 2},
		{[]byte("a\nb\n"), 2},
	}
	for _, tc := range tests {
		if got := countLines(tc.raw); got != tc.want {
			t.Fatalf("countLines(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestFnmatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"docs/**", "docs/guardrails.md", true},
		{"docs/**", "internal/foo.go", false},
		{"*_test.go", "internal/foo_test.go", true},
		{"*_test.go", "foo.go", false},
		{"vendor/**", "vendor/foo/bar", true},
		{"**/generated/**", "a/generated/x", true},
		{"*.min.js", "x.min.js", true},
		{"web/package-lock.json", "web/package-lock.json", true},
	}
	for _, tc := range tests {
		if got := fnmatch(tc.pattern, tc.path); got != tc.want {
			t.Fatalf("fnmatch(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMaxLinesFor(t *testing.T) {
	t.Parallel()
	if got := maxLinesFor("docs/guardrails.md", 300); got != 400 {
		t.Fatalf("docs limit = %d, want 400", got)
	}
	if got := maxLinesFor("internal/foo_test.go", 300); got != 500 {
		t.Fatalf("test limit = %d, want 500", got)
	}
	if got := maxLinesFor("internal/foo.go", 300); got != 300 {
		t.Fatalf("default limit = %d, want 300", got)
	}
	if got := maxLinesFor("devenv/mockllm/server.py", 300); got != 500 {
		t.Fatalf("devenv limit = %d, want 500", got)
	}
}

func TestExcluded(t *testing.T) {
	t.Parallel()
	if !excluded("vendor/foo/bar", defaultExcludes) {
		t.Fatal("expected vendor path to be excluded")
	}
	// Generated lockfiles are not hand-written source and are exempt.
	for _, path := range []string{"package-lock.json", "web/package-lock.json", "go.sum"} {
		if !excluded(path, defaultExcludes) {
			t.Fatalf("expected generated lockfile %q to be excluded", path)
		}
	}
	if excluded("internal/foo.go", defaultExcludes) {
		t.Fatal("expected internal path not to be excluded")
	}
}

// B-092: a file that is not staged yet must still be measured, or a local run
// passes on a tree CI rejects. Ignored files stay out.
func TestTrackedFilesIncludesUntrackedButNotIgnored(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("tracked.go")
	if out, err := exec.Command("git", "-C", dir, "add", "tracked.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	write("untracked.go")
	write("ignored.go")
	t.Chdir(dir)
	got := strings.Join(candidateFiles(), " ")
	if !strings.Contains(got, "untracked.go") || !strings.Contains(got, "tracked.go") || strings.Contains(got, "ignored.go") {
		t.Fatalf("files = %q; want tracked and untracked, not ignored", got)
	}
}
