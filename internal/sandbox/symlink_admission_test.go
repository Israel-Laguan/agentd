package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRefuseEscapingSymlinks(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}

	t.Run("outside link refused", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(ws, "link-out.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := refuseEscapingSymlinks(ws)
		if !IsSandboxViolation(err) {
			t.Fatalf("refuseEscapingSymlinks() = %v, want sandbox violation", err)
		}
	})

	t.Run("inside link passes", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, "real.txt"), []byte("x"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := os.Symlink("real.txt", filepath.Join(ws, "link-in.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := refuseEscapingSymlinks(ws); err != nil {
			t.Fatalf("refuseEscapingSymlinks() = %v, want nil for an inside link", err)
		}
	})

	t.Run("plain workspace passes", func(t *testing.T) {
		t.Parallel()
		if err := refuseEscapingSymlinks(t.TempDir()); err != nil {
			t.Fatalf("refuseEscapingSymlinks() = %v, want nil", err)
		}
	})

	t.Run("dangling link skipped", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		if err := os.Symlink(filepath.Join(ws, "gone.txt"), filepath.Join(ws, "dangling.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := refuseEscapingSymlinks(ws); err != nil {
			t.Fatalf("refuseEscapingSymlinks() = %v, want nil for an unresolvable link", err)
		}
	})
}
