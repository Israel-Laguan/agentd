package sandbox

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

// markerCommand writes a file in the workspace. A refused command never runs
// it, so the marker's absence is proof that nothing executed.
const markerName = "ran.marker"

var markerCommand = "touch " + markerName

// unusableJails are the two ways a jail can be broken: bwrap missing entirely,
// and bwrap present with unprivileged user namespaces unavailable.
var unusableJails = []struct {
	name       string
	bwrapFound bool
	userNS     bool
	reason     string
}{
	{"bwrap missing", false, false, "bwrap not found"},
	{"userns unavailable", true, false, "unprivileged user namespaces unavailable"},
}

func probeFor(bwrapFound, userNS bool, t *testing.T) jailProbeResult {
	t.Helper()
	probe := jailProbeResult{userNS: userNS}
	if bwrapFound {
		probe.path = fakeBwrap(t)
	}
	return probe
}

func markerExists(workspace string) bool {
	_, err := os.Stat(filepath.Join(workspace, markerName))
	return err == nil
}

// An explicit bubblewrap is a requirement: with no usable jail the command is
// refused before any shell starts, and the error says why.
func TestExplicitBubblewrapRefusesWhenJailUnusable(t *testing.T) {
	for _, tt := range unusableJails {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, probeFor(tt.bwrapFound, tt.userNS, t), func() {
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

// Auto keeps its documented fallback: the command runs filter-only.
func TestAutoRunsFilterOnlyWhenJailUnusable(t *testing.T) {
	for _, tt := range unusableJails {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, probeFor(tt.bwrapFound, tt.userNS, t), func() {
				exec, workspace := testExecutor(t, nil)
				result, err := exec.Execute(context.Background(), testPayload(workspace, markerCommand))
				if err != nil || !result.Success {
					t.Fatalf("Execute() err = %v, success = %v", err, result.Success)
				}
				if result.Backend != BackendFilterOnly {
					t.Fatalf("Result.Backend = %q, want %q", result.Backend, BackendFilterOnly)
				}
				if !markerExists(workspace) {
					t.Fatal("filter-only command did not run: marker file missing")
				}
			})
		})
	}
}

func TestExplicitBubblewrapRunsJailedWhenUsable(t *testing.T) {
	withProbe(t, jailProbeResult{path: fakeBwrap(t), userNS: true}, func() {
		exec, workspace := testExecutor(t, nil)
		exec.Backend = BackendBubblewrap
		result, err := exec.Execute(context.Background(), testPayload(workspace, markerCommand))
		if err != nil || !result.Success {
			t.Fatalf("Execute() err = %v, success = %v", err, result.Success)
		}
		if result.Backend != BackendBubblewrap {
			t.Fatalf("Result.Backend = %q, want %q", result.Backend, BackendBubblewrap)
		}
		if !markerExists(workspace) {
			t.Fatal("jailed command did not run: marker file missing")
		}
	})
}

func TestBackendStartupLevel(t *testing.T) {
	present := jailProbeResult{path: "/usr/bin/bwrap", userNS: true}
	broken := jailProbeResult{path: "/usr/bin/bwrap"}
	absent := jailProbeResult{}
	tests := []struct {
		name    string
		backend string
		probe   jailProbeResult
		want    slog.Level
	}{
		{"auto with jail", "", present, slog.LevelInfo},
		{"auto without bwrap", "", absent, slog.LevelInfo},
		{"auto with broken userns", "", broken, slog.LevelInfo},
		{"explicit filter-only", BackendFilterOnly, absent, slog.LevelInfo},
		{"explicit bubblewrap with jail", BackendBubblewrap, present, slog.LevelInfo},
		{"explicit bubblewrap without bwrap", BackendBubblewrap, absent, slog.LevelError},
		{"explicit bubblewrap with broken userns", BackendBubblewrap, broken, slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, tt.probe, func() {
				if got := BackendStartupLevel(tt.backend); got != tt.want {
					t.Fatalf("BackendStartupLevel(%q) = %v, want %v", tt.backend, got, tt.want)
				}
			})
		})
	}
}
