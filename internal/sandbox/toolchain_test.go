package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B-081: the jail binds only /usr, /lib, /bin and /sbin, so a toolchain
// installed elsewhere (Go in ~/.local/go) was "command not found" inside it
// and the worker looped trying to install one. A configured toolchain path is
// bound read-only and its bin directory goes on the jail's PATH.
func TestBubblewrapRunsAConfiguredToolchainOutsideUsr(t *testing.T) {
	requireJail(t)
	toolchain := fakeToolchain(t, "agentd-fake-go", "fake-go ok")
	exec, workspace := testExecutor(t, nil)
	exec.ToolchainPaths = []string{toolchain}

	result, err := exec.Execute(context.Background(), testPayload(workspace, "agentd-fake-go"))
	if err != nil || !result.Success {
		t.Fatalf("Execute(agentd-fake-go) err = %v, success = %v, stderr = %q", err, result.Success, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "fake-go ok") {
		t.Fatalf("stdout = %q, want the toolchain's output", result.Stdout)
	}
}

// The toolchain stays read-only: a command cannot change the host install.
func TestBubblewrapToolchainIsReadOnly(t *testing.T) {
	requireJail(t)
	toolchain := fakeToolchain(t, "agentd-fake-go", "fake-go ok")
	exec, workspace := testExecutor(t, nil)
	exec.ToolchainPaths = []string{toolchain}

	command := "touch " + filepath.Join(toolchain, "planted")
	_, _ = exec.Execute(context.Background(), testPayload(workspace, command))
	if _, err := os.Stat(filepath.Join(toolchain, "planted")); err == nil {
		t.Fatal("a jailed command wrote into the toolchain directory")
	}
}

func TestToolchainPathEntries(t *testing.T) {
	withBin := fakeToolchain(t, "tool", "x")
	flat := t.TempDir()
	got := toolchainPathEntries([]string{withBin, flat})
	want := []string{filepath.Join(withBin, "bin"), flat}
	if strings.Join(got, ":") != strings.Join(want, ":") {
		t.Fatalf("toolchainPathEntries() = %q, want %q", got, want)
	}
}

// fakeToolchain creates <dir>/bin/<name>, a script that prints output.
func fakeToolchain(t *testing.T, name, output string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho " + output + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil { //nolint:gosec // test toolchain must be executable
		t.Fatal(err)
	}
	return dir
}
