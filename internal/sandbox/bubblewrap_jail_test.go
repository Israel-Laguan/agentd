package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The S14 host-introspection collectors must keep working inside the jail:
// the command shapes the planner emits have to produce files the report
// renderer can parse. Markers mirror hostreport's parsers without importing
// it (sandbox tests must not depend on queue).
func TestBubblewrapCollectorsProduceParseableFacts(t *testing.T) {
	requireJail(t)
	ctx := context.Background()
	exec, workspace := testExecutor(t, nil)
	collectors := map[string]string{
		"os_info.txt":     "{ uname -a; hostnamectl; } > os_info.txt",
		"cpu_info.txt":    "lscpu > cpu_info.txt",
		"memory_info.txt": "free -h > memory_info.txt",
		"disk_info.txt":   "{ lsblk; df -h; } > disk_info.txt",
	}
	for name, command := range collectors {
		result, err := exec.Execute(ctx, testPayload(workspace, command))
		if err != nil || !result.Success {
			t.Fatalf("Execute(%q for %s) err = %v, success = %v", command, name, err, result.Success)
		}
	}
	facts := map[string][]string{
		"os_info.txt":     {"Linux", "Operating System:", "Kernel:"},
		"cpu_info.txt":    {"Model name:", "CPU(s):"},
		"memory_info.txt": {"Mem:"},
		"disk_info.txt":   {"TYPE", "disk"},
	}
	for name, markers := range facts {
		content, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(content), marker) {
				t.Fatalf("%s lacks %q:\n%s", name, marker, content)
			}
		}
	}
}

// The jailed shell must start in the workspace: the host cwd does not exist
// inside, so the executor pins --chdir to the bound workspace.
func TestBubblewrapKeepsWorkspaceCwd(t *testing.T) {
	requireJail(t)
	ctx := context.Background()
	exec, workspace := testExecutor(t, nil)
	result, err := exec.Execute(ctx, testPayload(workspace, "pwd"))
	if err != nil || !result.Success {
		t.Fatalf("Execute(pwd) err = %v, success = %v", err, result.Success)
	}
	if got := strings.TrimSpace(result.Stdout); got != workspace {
		t.Fatalf("pwd = %q, want workspace %q", got, workspace)
	}
}

// Filter-only keeps the admission checks: a workspace symlink pointing out
// is refused without any jail, and plain commands still run.
func TestFilterOnlyBackendPreservesAdmission(t *testing.T) {
	ctx := context.Background()
	exec, workspace := testExecutor(t, nil)
	exec.Backend = BackendFilterOnly
	result, err := exec.Execute(ctx, testPayload(workspace, "pwd"))
	if err != nil || !result.Success {
		t.Fatalf("Execute(pwd) err = %v, success = %v", err, result.Success)
	}
	outside, err := os.MkdirTemp("", "agentd-filter-outer-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	canary := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(canary, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(canary, filepath.Join(workspace, "link-out.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := exec.Execute(ctx, testPayload(workspace, "cp link-out.txt loot.txt")); !IsSandboxViolation(err) {
		t.Fatalf("Execute() = %v, want sandbox violation without a jail", err)
	}
}
