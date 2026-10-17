package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsOnly(t *testing.T) {
	t.Parallel()
	script, err := filepath.Abs("docs_only.sh")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		files []string
		base  string
		want  string
	}{
		{"docs tree", []string{"docs/guide.md", "docs/img/flow.png"}, "base", "true"},
		{"markdown anywhere", []string{"README.md", "web/README.md", "test/e2e/README.md"}, "base", "true"},
		{"docs tooling config", []string{".lychee.toml", ".markdownlint.json"}, "base", "true"},
		{"go file", []string{"internal/queue/worker/w.go"}, "base", "false"},
		{"docs plus code", []string{"docs/guide.md", "web/app/page.tsx"}, "base", "false"},
		{"non-markdown next to docs", []string{"config.reference.yaml"}, "base", "false"},
		{"no change", nil, "base", "false"},
		{"no base ref (push)", []string{"docs/guide.md"}, "", "false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			git(t, dir, 1000, "init", "-q", "-b", "main")
			commitFile(t, dir, "seed.txt", "seed", 1000)
			git(t, dir, 1000, "branch", "base")
			for _, f := range tc.files {
				if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o750); err != nil {
					t.Fatal(err)
				}
				commitFile(t, dir, f, "x", 2000)
			}
			args := []string{script}
			if tc.base != "" {
				args = append(args, tc.base)
			}
			cmd := exec.Command("bash", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GITHUB_BASE_REF=") // CI sets it on PRs
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("docs_only.sh: %v\n%s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != "docs_only="+tc.want {
				t.Fatalf("got %q, want docs_only=%s", got, tc.want)
			}
		})
	}
}
