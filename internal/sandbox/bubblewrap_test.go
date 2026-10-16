package sandbox

import (
	"strings"
	"testing"
)

func TestResolveBackend(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		want       string
	}{
		{"empty means auto", "", BackendAuto},
		{"auto", "auto", BackendAuto},
		{"bubblewrap", "bubblewrap", BackendBubblewrap},
		{"filter-only", "filter-only", BackendFilterOnly},
		{"case and space tolerant", "  BubbleWrap ", BackendBubblewrap},
		{"unknown falls back to auto", "podman", BackendAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveBackend(tt.configured); got != tt.want {
				t.Fatalf("resolveBackend(%q) = %q, want %q", tt.configured, got, tt.want)
			}
		})
	}
}

func TestJailEnvReplacesHostPaths(t *testing.T) {
	got := jailEnv([]string{"PATH=/home/u/bin:/usr/bin", "HOME=/home/u", "LANG=C.UTF-8", "CI=true"}, "/ws")
	joined := strings.Join(got, "\n")
	for _, want := range []string{"PATH=/usr/bin:/bin", "HOME=/ws", "LANG=C.UTF-8", "CI=true"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("jailEnv() = %q, want %q", got, want)
		}
	}
	if strings.Contains(joined, "/home/u") {
		t.Fatalf("jailEnv() = %q, leaks host paths", got)
	}
}

func TestJailEnvAddsDefaultsWhenAbsent(t *testing.T) {
	got := jailEnv([]string{"CI=true"}, "/ws")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "PATH=/usr/bin:/bin") || !strings.Contains(joined, "HOME=/ws") {
		t.Fatalf("jailEnv() = %q, want jail PATH and HOME defaults", got)
	}
}

// The argv must hide the host, keep the workspace writable at the same path,
// and apply mounts in order: the /tmp tmpfs would otherwise cover a workspace
// bound under /tmp.
func TestJailArgvProfile(t *testing.T) {
	argv := jailArgv("/usr/bin/bwrap", "/tmp/ws/p", []string{"CI=true"}, "echo hi")
	text := strings.Join(argv, "\n")
	for _, want := range []string{
		"--unshare-all", "--die-with-parent",
		"--ro-bind\n/usr\n/usr", "--ro-bind\n/lib\n/lib", "--ro-bind\n/lib64\n/lib64",
		"--ro-bind\n/bin\n/bin", "--ro-bind\n/sbin\n/sbin", "--ro-bind\n/sys\n/sys",
		"--ro-bind\n/run/dbus/system_bus_socket\n/run/dbus/system_bus_socket",
		"--ro-bind\n/run/systemd/system\n/run/systemd/system",
		"--proc\n/proc", "--dev\n/dev", "--tmpfs\n/tmp",
		"--bind\n/tmp/ws/p\n/tmp/ws/p", "--chdir\n/tmp/ws/p",
		"--clearenv", "--setenv\nCI\ntrue", "--setenv\nPATH\n/usr/bin:/bin",
		"bash", "echo hi",
	} {
		flat := strings.ReplaceAll(text, "\n", " ")
		if !strings.Contains(flat, strings.ReplaceAll(want, "\n", " ")) {
			t.Fatalf("argv lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "/home") || strings.Contains(text, "/etc") {
		t.Fatalf("argv exposes host paths:\n%s", text)
	}
	tmpfs := strings.Index(text, "--tmpfs")
	bind := strings.Index(text, "--bind\n/tmp/ws/p")
	if tmpfs < 0 || bind < 0 || tmpfs > bind {
		t.Fatalf("tmpfs must precede the workspace bind:\n%s", text)
	}
}

// withProbe runs f with the jail probe stubbed. The stub is process-global,
// so callers must not run in parallel.
func withProbe(t *testing.T, status jailProbeResult, f func()) {
	t.Helper()
	old := probeJail
	probeJail = func() jailProbeResult { return status }
	t.Cleanup(func() { probeJail = old })
	f()
}

func TestUseJailDecisionMatrix(t *testing.T) {
	present := jailProbeResult{path: "/usr/bin/bwrap", userNS: true}
	broken := jailProbeResult{path: "/usr/bin/bwrap", userNS: false}
	absent := jailProbeResult{}
	tests := []struct {
		name    string
		backend string
		probe   jailProbeResult
		want    bool
	}{
		{"auto with jail", "", present, true},
		{"auto without binary", "", absent, false},
		{"auto with broken userns", "", broken, false},
		{"explicit jail available", BackendBubblewrap, present, true},
		{"explicit jail broken falls back", BackendBubblewrap, broken, false},
		{"filter-only never jails", BackendFilterOnly, present, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, tt.probe, func() {
				exec := &BashExecutor{Backend: tt.backend}
				_, ok := exec.useJail("ws", testPayload("ws", "true"))
				if ok != tt.want {
					t.Fatalf("useJail() = %v, want %v", ok, tt.want)
				}
			})
		})
	}
}
