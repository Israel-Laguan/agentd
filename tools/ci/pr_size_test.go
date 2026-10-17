package ci_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command("bash", append([]string{"pr_size.sh"}, args...)...).CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	default:
		t.Fatal(err)
		return -1, ""
	}
}

func TestPRSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"well under limits", []string{"5", "100", "20", ""}, 0, ""},
		{"99 files passes", []string{"99", "10", "10", ""}, 0, ""},
		{"100 files fails", []string{"100", "10", "10", ""}, 1, "files"},
		{"1000 changed lines passes", []string{"10", "600", "400", ""}, 0, ""},
		{"1001 changed lines fails", []string{"10", "600", "401", ""}, 1, "lines"},
		{"deletions count toward lines", []string{"10", "0", "1001", ""}, 1, "lines"},
		{"additions count toward lines", []string{"10", "1001", "0", ""}, 1, "lines"},
		{"label overrides both limits", []string{"150", "5000", "600", "bug,large-pr-ok"}, 0, "large-pr-ok"},
		{"similar label does not override", []string{"150", "10", "10", "large-pr-okay,not-large-pr-ok"}, 1, "files"},
		{"label absent from list fails", []string{"150", "10", "10", "bug"}, 1, "files"},
		{"too few args is a usage error", []string{"1", "2"}, 2, "usage"},
		{"non-numeric input is a usage error", []string{"x", "1", "1", ""}, 2, "usage"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out := run(t, tc.args...)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.want, out)
			}
			if !strings.Contains(out, tc.msg) {
				t.Fatalf("output missing %q:\n%s", tc.msg, out)
			}
		})
	}
}

func gitRun(t *testing.T, dir, base, head, labels string) (int, string) {
	t.Helper()
	abs, err := filepath.Abs("pr_size_test.go")
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(abs)))
	script := filepath.Join(repoRoot, "tools", "ci", "pr_size.sh")
	t.Logf("gitRun: dir=%q base=%q head=%q labels=%q script=%s", dir, base, head, labels, script)
	cmd := exec.Command("bash", "-x", script, base, head, labels)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PR_GIT=1", "BASE_SHA="+base, "HEAD_SHA="+head)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	t.Logf("gitRun: script exit=%d full output:\n%s", code, out.String())
	return code, out.String()
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func makeCommit(t *testing.T, dir, msg string) string {
	t.Helper()
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-qm", msg)
	return commitRev(t, dir)
}

func commitRev(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestPRSizeGit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string) (string, string)
		labels   string
		wantCode int
		wantOut  string
	}{
		{"deletion-heavy lockfile churn excluded", func(t *testing.T, dir string) (string, string) {
			gitCmd(t, dir, "init", "-q")
			gitCmd(t, dir, "config", "user.email", "t@t")
			gitCmd(t, dir, "config", "user.name", "t")
			writeFile(t, dir, "a.go", "package main\nfunc A() {}\n")
			writeFile(t, dir, "package-lock.json", strings.Repeat("x\n", 1200))
			base := makeCommit(t, dir, "base")
			writeFile(t, dir, "package-lock.json", strings.Repeat("y\n", 36))
			writeFile(t, dir, "api.ts", "import \"agentd\"\n")
			head := makeCommit(t, dir, "head")
			return base, head
		}, "bug", 0, "1 files, 1 changed lines; excluded 1 generated files"},
		{"real deletions still counted and fail", func(t *testing.T, dir string) (string, string) {
			gitCmd(t, dir, "init", "-q")
			gitCmd(t, dir, "config", "user.email", "t@t")
			gitCmd(t, dir, "config", "user.name", "t")
			writeFile(t, dir, "big.go", strings.Repeat("z\n", 1001))
			base := makeCommit(t, dir, "add-big")
			if err := os.Remove(filepath.Join(dir, "big.go")); err != nil {
				t.Fatal(err)
			}
			head := makeCommit(t, dir, "del-big")
			return base, head
		}, "bug", 1, "1001 changed lines"},
		{"large-pr-ok label overrides the line limit", func(t *testing.T, dir string) (string, string) {
			gitCmd(t, dir, "init", "-q")
			gitCmd(t, dir, "config", "user.email", "t@t")
			gitCmd(t, dir, "config", "user.name", "t")
			writeFile(t, dir, "big.go", strings.Repeat("z\n", 1001))
			base := makeCommit(t, dir, "add-big")
			if err := os.Remove(filepath.Join(dir, "big.go")); err != nil {
				t.Fatal(err)
			}
			head := makeCommit(t, dir, "del-big")
			return base, head
		}, "large-pr-ok", 0, "over limit, overridden"},
		{"missing base object is a usage error with a hint", func(t *testing.T, dir string) (string, string) {
			gitCmd(t, dir, "init", "-q")
			gitCmd(t, dir, "config", "user.email", "t@t")
			gitCmd(t, dir, "config", "user.name", "t")
			writeFile(t, dir, "a.go", "package main\n")
			head := makeCommit(t, dir, "only")
			return "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", head
		}, "bug", 2, "fetch-depth: 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			base, head := tc.setup(t, tmp)
			code, out := gitRun(t, tmp, base, head, tc.labels)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("output missing %q:\n%s", tc.wantOut, out)
			}
		})
	}
}
