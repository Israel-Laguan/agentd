package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// git runs git in dir with a fixed committer date (unix seconds).
func git(t *testing.T, dir string, date int64, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	stamp := time.Unix(date, 0).UTC().Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitFile writes name and commits it at date.
func commitFile(t *testing.T, dir, name, body string, date int64) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, date, "add", name)
	git(t, dir, date, "commit", "-q", "-m", name)
}

func mtime(t *testing.T, dir, name string) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime().Unix()
}

func TestRestoreMtimes(t *testing.T) {
	t.Parallel()
	script, err := filepath.Abs("restore_mtimes.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	git(t, dir, 1000, "init", "-q", "-b", "main")
	commitFile(t, dir, "a.txt", "a1", 1000)
	commitFile(t, dir, "b.txt", "b1", 2000)
	if err := os.MkdirAll(filepath.Join(dir, "d", "e"), 0o750); err != nil {
		t.Fatal(err)
	}
	commitFile(t, dir, "d/e/f.txt", "f1", 2500)
	commitFile(t, dir, "d/g.txt", "g1", 1500)
	commitFile(t, dir, "a.txt", "a2", 3000)
	git(t, dir, 3000, "branch", "base")
	commitFile(t, dir, "c.txt", "c1", 4000)

	run := func(args ...string) {
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GITHUB_BASE_REF=") // CI sets it on PRs
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("restore_mtimes.sh %v: %v\n%s", args, err, out)
		}
	}

	run()
	for name, want := range map[string]int64{
		"a.txt": 3000, "b.txt": 2000, "c.txt": 4000, "d/e/f.txt": 2500, "d/g.txt": 1500,
		"d/e": 2500, "d": 2500, // a directory takes the newest commit time under it
	} {
		if got := mtime(t, dir, name); got != want {
			t.Errorf("%s mtime = %d, want its last commit time %d", name, got, want)
		}
	}

	before := time.Now().Add(-time.Minute).Unix()
	run("base")
	if got := mtime(t, dir, "c.txt"); got < before {
		t.Errorf("c.txt differs from base: mtime = %d, want touched to now", got)
	}
	if got := mtime(t, dir, "b.txt"); got != 2000 {
		t.Errorf("b.txt matches base: mtime = %d, want 2000", got)
	}
}
