package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

// withRealProbe runs f with the uncached real probe, so the PATH each test sets
// is what the probe sees. The stub is process-global, like withProbe.
func withRealProbe(t *testing.T, f func()) {
	t.Helper()
	old := probeJail
	probeJail = probeJailHost
	t.Cleanup(func() { probeJail = old })
	f()
}

// A refusal on a real host, with no stubbed probe (B-051 gap A). PATH decides
// what the probe finds: an empty directory has no bwrap, and a bwrap that exits
// non-zero stands in for a kernel that refuses the user namespace. On either, an
// explicit bubblewrap must refuse before any shell starts. It needs no working
// jail, so it runs on CI as well.
func TestExplicitBubblewrapRefusesOnRealProbe(t *testing.T) {
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "bwrap"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(bwrap) error = %v", err)
	}
	tests := []struct {
		name   string
		dir    string
		reason string
	}{
		{"bwrap missing", t.TempDir(), "bwrap not found"},
		{"userns refused by bwrap", broken, "unprivileged user namespaces unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", tt.dir)
			withRealProbe(t, func() {
				exec, workspace := testExecutor(t, nil)
				exec.Backend = BackendBubblewrap
				result, err := exec.Execute(context.Background(), testPayload(workspace, markerCommand))
				if !errors.Is(err, models.ErrSandboxUnavailable) {
					t.Fatalf("Execute() err = %v, want ErrSandboxUnavailable", err)
				}
				if !strings.Contains(err.Error(), tt.reason) {
					t.Fatalf("Execute() err = %q, want reason %q", err, tt.reason)
				}
				if result.Backend != "" {
					t.Fatalf("Result.Backend = %q, want empty: no backend ran", result.Backend)
				}
				if markerExists(workspace) {
					t.Fatal("refused command ran: marker file exists")
				}
			})
		})
	}
}
