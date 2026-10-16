package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBwrap writes a stand-in bwrap that prints a marker, then runs the shell
// command jailArgv puts last with no namespace at all. It drives the jail
// branch of the executor on machines without bubblewrap.
func fakeBwrap(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bwrap")
	script := "#!/bin/sh\necho via-fake-bwrap\nfor last; do :; done\nexec bash -c \"$last\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(bwrap) error = %v", err)
	}
	return path
}

func TestBackendStartupLine(t *testing.T) {
	present := jailProbeResult{path: "/usr/bin/bwrap", userNS: true}
	broken := jailProbeResult{path: "/usr/bin/bwrap"}
	absent := jailProbeResult{}
	tests := []struct {
		name    string
		backend string
		probe   jailProbeResult
		want    string
	}{
		{"auto with jail", "", present, "sandbox backend=bubblewrap (bwrap and userns available)"},
		{"auto without bwrap", "", absent, "sandbox backend=filter-only (bwrap not found)"},
		{"auto with broken userns", "", broken, "sandbox backend=filter-only (unprivileged user namespaces unavailable)"},
		{"explicit filter-only", BackendFilterOnly, present, "sandbox backend=filter-only (configured filter-only)"},
		{"explicit bubblewrap with jail", BackendBubblewrap, present, "sandbox backend=bubblewrap (bwrap and userns available)"},
		{"explicit bubblewrap without bwrap", BackendBubblewrap, absent, "sandbox backend=bubblewrap REQUIRED but unavailable (bwrap not found); commands will be refused"},
		{"explicit bubblewrap with broken userns", BackendBubblewrap, broken, "sandbox backend=bubblewrap REQUIRED but unavailable (unprivileged user namespaces unavailable); commands will be refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, tt.probe, func() {
				if got := BackendStartupLine(tt.backend); got != tt.want {
					t.Fatalf("BackendStartupLine(%q) = %q, want %q", tt.backend, got, tt.want)
				}
			})
		})
	}
}

// The backend that ran must come back on the Result, because the worker
// records it from there. Each row pins one cell of the auto/explicit matrix
// by what actually executed: the Backend field and whether the jail branch
// ran (the fake bwrap prints its marker only when it is invoked).
func TestExecutionResultRecordsBackend(t *testing.T) {
	tests := []struct {
		name        string
		backend     string
		bwrapFound  bool
		userNS      bool
		wantBackend string
		wantJailed  bool
	}{
		{"auto with working jail", "", true, true, BackendBubblewrap, true},
		{"auto without bwrap", "", false, false, BackendFilterOnly, false},
		{"auto with broken userns", "", true, false, BackendFilterOnly, false},
		{"explicit filter-only with working jail", BackendFilterOnly, true, true, BackendFilterOnly, false},
		{"explicit bubblewrap with working jail", BackendBubblewrap, true, true, BackendBubblewrap, true},
		// An explicit bubblewrap with a broken jail is refused, not run; see backend_required_test.go.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := jailProbeResult{userNS: tt.userNS}
			if tt.bwrapFound {
				probe.path = fakeBwrap(t)
			}
			withProbe(t, probe, func() {
				exec, workspace := testExecutor(t, nil)
				exec.Backend = tt.backend
				result, err := exec.Execute(context.Background(), testPayload(workspace, "pwd"))
				if err != nil || !result.Success {
					t.Fatalf("Execute(pwd) err = %v, success = %v", err, result.Success)
				}
				if result.Backend != tt.wantBackend {
					t.Fatalf("Result.Backend = %q, want %q", result.Backend, tt.wantBackend)
				}
				if jailed := strings.Contains(result.Stdout, "via-fake-bwrap"); jailed != tt.wantJailed {
					t.Fatalf("jailed = %v, want %v; stdout = %q", jailed, tt.wantJailed, result.Stdout)
				}
			})
		})
	}
}
