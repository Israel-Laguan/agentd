package hostreport

import (
	"errors"
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
	writeFile(t, dir, "cpu_info.txt", lscpuOut)
	writeFile(t, dir, "mem.txt", freeOut)
	writeFile(t, dir, "disk_info.txt", lsblkOut)
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
	if strings.Contains(got, "not collected") {
		t.Fatalf("complete collection still has gaps:\n%s", got)
	}
}

// B-046: a partial collection writes nothing and names the missing facts plus
// the files they were looked for in, so the worker fails the task instead of
// completing a gapped report.
func TestWriteReportRefusesPartialFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "os_info.txt", hostnamectlOut+unameOut)
	writeFile(t, dir, "mem.txt", freeOut)
	_, ok, err := WriteReport(dir, "report.md")
	if ok || err == nil {
		t.Fatalf("WriteReport() ok %v err %v, want a missing-facts refusal", ok, err)
	}
	var missing *MissingFactsError
	if !errors.As(err, &missing) {
		t.Fatalf("WriteReport() err = %T, want *MissingFactsError", err)
	}
	if len(missing.Missing) != 2 || missing.Missing[0] != "CPU" || missing.Missing[1] != "disks" {
		t.Fatalf("missing = %q, want [CPU disks]", missing.Missing)
	}
	if len(missing.Files) != 2 || missing.Files[0] != "mem.txt" || missing.Files[1] != "os_info.txt" {
		t.Fatalf("files = %q, want the sorted files read", missing.Files)
	}
	if _, serr := os.Stat(filepath.Join(dir, "report.md")); serr == nil {
		t.Fatal("report.md was written for partial facts")
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
	writeFile(t, dir, "os_info.txt", hostnamectlOut+unameOut)
	writeFile(t, dir, "cpu_info.txt", lscpuOut)
	writeFile(t, dir, "disk_info.txt", lsblkOut)
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

// An existing regular target belongs to the model path: overwriting a NOTES.md
// the user owns (or a report from an earlier attempt) with a system report is
// never the renderer's call.
func TestWriteReportLeavesExistingFileToModel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "os_info.txt", hostnamectlOut+unameOut)
	writeFile(t, dir, "cpu_info.txt", lscpuOut)
	writeFile(t, dir, "mem.txt", freeOut)
	writeFile(t, dir, "disk_info.txt", lsblkOut)
	writeFile(t, dir, "report.md", "my notes")
	if _, ok, err := WriteReport(dir, "report.md"); ok || err != nil {
		t.Fatalf("WriteReport() ok %v err %v, want ok=false for an existing file", ok, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "report.md")); string(got) != "my notes" {
		t.Fatalf("report.md = %q, want the existing content untouched", got)
	}
}
