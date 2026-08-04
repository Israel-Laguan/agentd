package main

import "testing"

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
