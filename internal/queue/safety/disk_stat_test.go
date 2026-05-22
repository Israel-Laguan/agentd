package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskFreePercent_TempDir(t *testing.T) {
	pct, err := DiskFreePercent(os.TempDir())
	if err != nil {
		t.Fatalf("DiskFreePercent: %v", err)
	}
	if pct < 0 || pct > 100 {
		t.Fatalf("free percent = %v, want 0-100", pct)
	}
}

func TestDiskFreePercent_InvalidPath(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "definitely-missing", "path")
	_, err := DiskFreePercent(invalid)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}
