package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// B-042 / SP-019 escape probes. Each test states the DESIRED behaviour
// (the route out of the workspace must be refused) against a throwaway
// temp workspace and throwaway canary files — never real host files.
// Probe 1 (symlink-out) is enforced at admission and runs everywhere; probes
// 2-4 need the bubblewrap jail and run wherever it is available (they skip
// where the binary or user namespaces are missing, so `make check` stays
// green there; CI installs bubblewrap — see .github/workflows/ci-go.yml).

// escapeWorkspace sets up a throwaway workspace plus a canary file outside
// it, and returns the executor, workspace, canary path and canary content.
func escapeWorkspace(t *testing.T) (*BashExecutor, string, string, string) {
	t.Helper()
	exec, workspace := testExecutor(t, nil)
	outside, err := os.MkdirTemp("", "agentd-escape-outer-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	const canary = "AGENTD-ESCAPE-CANARY-7f3a9c"
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte(canary+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return exec, workspace, filepath.Join(outside, "secret.txt"), canary
}

// requireJail skips the test when the bubblewrap jail is unavailable on this
// platform: the remaining probes assert jail behavior and cannot run
// filter-only. A skip means the platform cannot jail, never a pass. On CI the
// same condition fails the test instead (see jailVerdict).
func requireJail(t *testing.T) {
	t.Helper()
	status := probeJail()
	if status.usable() {
		return
	}
	fail, msg := jailVerdict(status)
	if fail {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

// jailVerdict decides what an unusable jail means for a jail test. GitHub
// Actions sets CI, and there the runner must jail, so a skip would hide a
// broken jail: fail with the probe's reason. Elsewhere a skip is valid.
func jailVerdict(status jailProbeResult) (bool, string) {
	msg := fmt.Sprintf("bubblewrap jail unavailable: bwrap path=%q userns=%t", status.path, status.userNS)
	if os.Getenv("CI") != "" {
		return true, msg + " (CI must run the jail tests, not skip them; see B-049)"
	}
	return false, msg + " on this platform"
}
// slashFree rewrites an absolute path so the command text holds no literal
// `/`: `s` is seeded once from `$(printf '\057')` and every separator
// becomes `${s}`. The shell assembles the real path at run time, which the
// lexical filter cannot see.
func slashFree(path string) string {
	return "${s}" + strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "${s}")
}

func slashSeed() string {
	return "s=$(printf '\\057'); "
}

// assertNoEscape fails the test when a refused-or-contained outcome did not
// happen: either the command was refused at admission (desired), or it ran
// but the loot file must not hold the canary.
func assertNoEscape(t *testing.T, ctx context.Context, exec *BashExecutor, workspace, command, loot, canary string) {
	t.Helper()
	result, err := exec.Execute(ctx, testPayload(workspace, command))
	if IsSandboxViolation(err) {
		return
	}
	if err != nil {
		t.Fatalf("Execute(%q) error = %v, want sandbox refusal or containment", command, err)
	}
	_ = result
	content, readErr := os.ReadFile(filepath.Join(workspace, loot))
	if readErr != nil {
		t.Fatalf("Execute(%q) ran but loot %q unreadable: %v", command, loot, readErr)
	}
	if strings.Contains(string(content), canary) {
		t.Fatalf("ESCAPE via %q: canary leaked into %q", command, loot)
	}
}

// 1. Symlink inside the workspace pointing outside, read by relative name
// (seeded the way source_path materialization can plant one). Fixed by the
// admission check (B-042 slice 1): workspace symlinks resolving outside are
// refused before execution, so this probe is a real acceptance test.
func TestSandboxEscapeProbe_SymlinkOut(t *testing.T) {
	ctx := context.Background()
	exec, workspace, canaryFile, canary := escapeWorkspace(t)
	content, err := os.ReadFile(canaryFile)
	if err != nil || !strings.Contains(string(content), canary) {
		t.Fatalf("canary setup failed: %v", err)
	}
	if err := os.Symlink(canaryFile, filepath.Join(workspace, "link-out.txt")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	assertNoEscape(t, ctx, exec, workspace, "cp link-out.txt loot2.txt", "loot2.txt", canary)
}

// 2. Paths assembled at run time against a canary outside the workspace:
// command substitution, variable concatenation, and glob expansion.
func TestSandboxEscapeProbe_RuntimeBuiltPath(t *testing.T) {
	requireJail(t)
	ctx := context.Background()
	exec, workspace, canaryFile, canary := escapeWorkspace(t)

	t.Run("command substitution", func(t *testing.T) {
		assertNoEscape(t, ctx, exec, workspace,
			slashSeed()+"cat "+slashFree(canaryFile)+" > loot-sub.txt", "loot-sub.txt", canary)
	})

	t.Run("variable concatenation", func(t *testing.T) {
		dir, file := filepath.Split(strings.TrimPrefix(canaryFile, "/"))
		var build strings.Builder
		build.WriteString(slashSeed())
		build.WriteString("d=${s}; ")
		for _, part := range strings.Split(strings.TrimSuffix(dir, "/"), "/") {
			build.WriteString("d=${d}" + part + "${s}; ")
		}
		assertNoEscape(t, ctx, exec, workspace,
			build.String()+"f=${d}"+file+"; cat ${f} > loot-concat.txt", "loot-concat.txt", canary)
	})

	t.Run("glob", func(t *testing.T) {
		outer := filepath.Dir(canaryFile)
		parent := slashFree(filepath.Dir(outer))
		base := filepath.Base(outer)
		prefix := base[:strings.LastIndex(base, "-")]
		assertNoEscape(t, ctx, exec, workspace,
			slashSeed()+"cat "+parent+"${s}"+prefix+"-*/secret.txt > loot-glob.txt",
			"loot-glob.txt", canary)
	})
}

// 3. /proc/self/root/<canary path> alias reaches the same outside file.
func TestSandboxEscapeProbe_ProcSelfRoot(t *testing.T) {
	requireJail(t)
	ctx := context.Background()
	exec, workspace, canaryFile, canary := escapeWorkspace(t)
	alias := filepath.Join("/proc/self/root", canaryFile)
	assertNoEscape(t, ctx, exec, workspace,
		slashSeed()+"cat "+slashFree(alias)+" > loot5.txt", "loot5.txt", canary)
}

// 4. /proc/<pid>/environ of a helper child owned by the test. The child
// carries ONLY a fake marker (minimal env, no os.Environ inheritance), so
// no real daemon variable or LITELLM_API_KEY can reach the probe output.
// The marker deliberately avoids secret-shaped text: the default scrubber
// redacts `SECRET=...` shapes from captured output, which would mask the
// read itself. A non-secret-shaped marker proves arbitrary environ bytes
// are readable by the sandboxed command.
func TestSandboxEscapeProbe_ProcPidEnviron(t *testing.T) {
	requireJail(t)
	const fakeMarker = "AGENTD_FAKE_CANARY=canary-123"
	helper := exec.Command("sleep", "60")
	helper.Env = []string{fakeMarker, "PATH=/usr/bin:/bin"}
	if err := helper.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = helper.Process.Kill()
		_ = helper.Wait()
	}()
	pid := helper.Process.Pid

	ctx := context.Background()
	exec, workspace := testExecutor(t, nil)
	target := "/proc/" + strconv.Itoa(pid) + "/environ"

	// Literal form: the lexical filter refuses this (documents the control).
	if err := validateCommandPaths("cat "+target, workspace); err == nil {
		t.Fatalf("validateCommandPaths(%q) = nil, want sandbox violation", target)
	}

	// Run-time-built form: the filter sees no literal path.
	command := slashSeed() + "cat " + slashFree(target)
	result, err := exec.Execute(ctx, testPayload(workspace, command))
	if IsSandboxViolation(err) {
		return
	}
	if err != nil {
		t.Fatalf("Execute(%q) error = %v", command, err)
	}
	if strings.Contains(result.Stdout, "canary-123") {
		t.Fatal("ESCAPE via /proc/<pid>/environ: fake marker leaked to stdout")
	}
}

// Guard (passes today, stays unskipped): the sandbox's own environment
// carries none of the daemon's variables.
func TestSandboxDoesNotInheritDaemonEnv(t *testing.T) {
	t.Setenv("AGENTD_DAEMON_MARKER_VAR", "daemon-marker-xyz-987")
	exec, workspace := testExecutor(t, nil)
	result, err := exec.Execute(context.Background(), testPayload(workspace, "env"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Contains(result.Stdout, "daemon-marker-xyz-987") {
		t.Fatalf("sandbox env inherited daemon variable: %q", result.Stdout)
	}
}
