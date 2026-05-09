package frontdesk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/gateway"
)

func TestFileStashBelowThresholdKeepsInline(t *testing.T) {
	fs := &FileStash{Dir: t.TempDir(), StashThreshold: 10}
	path, err := fs.Stash("short")
	if err != nil {
		t.Fatalf("Stash() error = %v", err)
	}
	if path != "" {
		t.Fatalf("Stash() path = %q, want empty", path)
	}
}

func TestFileStashAboveThresholdWritesFile(t *testing.T) {
	fs := &FileStash{Dir: t.TempDir(), StashThreshold: 5}
	path, err := fs.Stash("long enough")
	if err != nil {
		t.Fatalf("Stash() error = %v", err)
	}
	if path == "" {
		t.Fatalf("Stash() path empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stashed file: %v", err)
	}
	if string(data) != "long enough" {
		t.Fatalf("stashed content = %q, want original", string(data))
	}
}

func TestFileStashReadTruncatesWithStrategy(t *testing.T) {
	dir := t.TempDir()
	input := strings.Repeat("a", 50) + strings.Repeat("b", 50)
	path := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	fs := &FileStash{Dir: dir}
	got, err := fs.Read(path, gateway.HeadTailStrategy{HeadRatio: 1}, 40)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 40 {
		t.Fatalf("len(Read()) = %d, want 40", len(got))
	}
	if !strings.HasPrefix(got, "aaaaaaaaaa") {
		t.Fatalf("Read() = %q, want preserved head", got)
	}
	if strings.HasSuffix(got, "bbbbbbbbbb") {
		t.Fatalf("Read() = %q, did not expect tail for head-only strategy", got)
	}
}

func TestFileStashReadRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	fs := &FileStash{Dir: dir}
	_, err := fs.Read(outside, gateway.MiddleOutStrategy{}, 20)
	if err == nil {
		t.Fatalf("Read() error = nil, want jail error")
	}
}
