package hostreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestWriteReportFromWorkspace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "os_info.txt", hostnamectlOut+unameOut)
	writeFile(t, dir, "mem.txt", freeOut)
	got, ok, err := WriteReport(dir, "report.md")
	if err != nil || !ok {
		t.Fatalf("WriteReport() = ok %v, err %v", ok, err)
	}
	onDisk, rerr := os.ReadFile(filepath.Join(dir, "report.md"))
	if rerr != nil || string(onDisk) != got {
		t.Fatalf("report.md on disk = %q (%v), want %q", onDisk, rerr, got)
	}
	if strings.Contains(got, "Machine ID") || strings.Contains(got, "report.md") {
		t.Fatalf("report mentions identifiers or itself:\n%s", got)
	}
}

// A symlink planted by a sandboxed command must never make the daemon read or
// overwrite a file outside the workspace (SP-019).
func TestWriteReportIgnoresSymlinks(t *testing.T) {
	t.Parallel()
	dir, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	writeFile(t, outside, "secret.txt", "Operating System: LEAKED\n")
	if err := os.Symlink(secret, filepath.Join(dir, "linked.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeFile(t, dir, "mem.txt", freeOut)
	got, ok, err := WriteReport(dir, "report.md")
	if err != nil || !ok || strings.Contains(got, "LEAKED") {
		t.Fatalf("WriteReport() = %q ok %v err %v, want no leaked symlink content", got, ok, err)
	}

	target := filepath.Join(outside, "victim.md")
	writeFile(t, outside, "victim.md", "keep")
	if err := os.Symlink(target, filepath.Join(dir, "summary.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, ok, _ := WriteReport(dir, "summary.md"); ok {
		t.Fatal("WriteReport() wrote through a symlinked output, want a refusal")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatalf("symlink target modified: %q", b)
	}
}

func TestWriteReportWithoutFactsWritesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "notes.txt", "nothing useful")
	if _, ok, err := WriteReport(dir, "report.md"); ok || err != nil {
		t.Fatalf("WriteReport() ok %v err %v, want ok=false", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "report.md")); err == nil {
		t.Fatal("report.md was created without facts")
	}
}
