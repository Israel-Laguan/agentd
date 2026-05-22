package safety

import (
	"os"
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
	_, err := DiskFreePercent("/nonexistent-agentd-coverage-path-xyz")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}
