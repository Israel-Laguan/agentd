package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repoWith builds a throwaway repo: a base commit (optionally already carrying
// a trailer, as main's history does), tagged "base", then one commit with msg.
func repoWith(t *testing.T, baseMsg, msg string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	commit := func(name, m string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "add", name)
		git(t, dir, "commit", "-q", "-m", m)
	}
	commit("a", baseMsg)
	git(t, dir, "tag", "base")
	if msg != "" {
		commit("b", msg)
	}
	return dir
}

func TestOffenders(t *testing.T) {
	t.Parallel()
	const trailer = "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
	tests := []struct {
		name, baseMsg, msg string
		want               int
	}{
		{"real trailer is rejected", "base", "feat: x\n\nbody\n\n" + trailer, 1},
		{"lowercase key trailer is rejected", "base", "feat: x\n\nco-authored-by: A <a@a>", 1},
		{"prose quoting the trailer at column 0 is accepted", "base", "docs: x\n\nThe rule bans\nCo-Authored-By: Someone <s@s>\nin commits, as prose.\n\nSigned-off-by: t <t@t>", 0},
		{"clean commit is accepted", "base", "feat: clean", 0},
		{"trailer already on base is exempt", "base\n\n" + trailer, "feat: clean", 0},
		{"no commits past base passes", "base\n\n" + trailer, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := repoWith(t, tc.baseMsg, tc.msg)
			got, err := offenders(dir, "base")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("offenders = %v, want %d", got, tc.want)
			}
		})
	}
}

func TestOffendersBadBase(t *testing.T) {
	t.Parallel()
	if _, err := offenders(repoWith(t, "base", ""), "nope"); err == nil {
		t.Fatal("unknown base must be an error, not a silent pass")
	}
}
