package sandbox

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"
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
	got := jailEnv([]string{"PATH=/home/u/bin:/usr/bin", "HOME=/home/u", "LANG=C.UTF-8", "CI=true"}, "/ws", nil)
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
	got := jailEnv([]string{"CI=true"}, "/ws", nil)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "PATH=/usr/bin:/bin") || !strings.Contains(joined, "HOME=/ws") {
		t.Fatalf("jailEnv() = %q, want jail PATH and HOME defaults", got)
	}
}

// The argv must hide the host, keep the workspace writable at the same path,
// and apply mounts in order: the /tmp tmpfs would otherwise cover a workspace
// bound under /tmp.
func TestJailArgvProfile(t *testing.T) {
	argv := jailArgv("/usr/bin/bwrap", "/tmp/ws/p", false, []string{"CI=true"}, nil, nil, "echo hi")
	text := strings.Join(argv, "\n")
	for _, want := range []string{
		"--unshare-all", "--die-with-parent",
		"--ro-bind\n/usr\n/usr", "--ro-bind\n/lib\n/lib", "--ro-bind-try\n/lib64\n/lib64",
		"--ro-bind\n/bin\n/bin", "--ro-bind\n/sbin\n/sbin", "--ro-bind\n/sys\n/sys",
		"--ro-bind-try\n/run/dbus/system_bus_socket\n/run/dbus/system_bus_socket",
		"--ro-bind-try\n/run/systemd/system\n/run/systemd/system",
		"--ro-bind-try\n/etc/resolv.conf\n/etc/resolv.conf",
		"--ro-bind-try\n/etc/hosts\n/etc/hosts",
		"--ro-bind-try\n/etc/nsswitch.conf\n/etc/nsswitch.conf",
		"--ro-bind-try\n/etc/ssl/certs\n/etc/ssl/certs",
		"--ro-bind-try\n/etc/os-release\n/etc/os-release",
		"--ro-bind-try\n/etc/passwd\n/etc/passwd",
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
	if strings.Contains(text, "/home") || strings.Contains(text, "/etc/machine-id") || strings.Contains(text, "/etc/shadow") {
		t.Fatalf("argv exposes host paths:\n%s", text)
	}
	if strings.Contains(text, "--share-net") {
		t.Fatalf("argv shares the host network (B-086):\n%s", text)
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
		{"filter-only never jails", BackendFilterOnly, present, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, tt.probe, func() {
				exec := &BashExecutor{Backend: tt.backend}
				_, ok, err := exec.useJail("ws", testPayload("ws", "true"))
				if err != nil || ok != tt.want {
					t.Fatalf("useJail() = %v, err = %v, want %v", ok, err, tt.want)
				}
			})
		})
	}
}

// B-086 guard: without egress the argv is byte-for-byte the builtin profile,
// captured from main at f81ed8bc before the egress option existed.
func TestJailArgvWithoutEgressIsUnchanged(t *testing.T) {
	profile := `"/usr/bin/bwrap" "--unshare-all" "--die-with-parent" "--ro-bind" "/usr" "/usr" "--ro-bind" "/lib" "/lib" ` +
		`"--ro-bind-try" "/lib64" "/lib64" "--ro-bind" "/bin" "/bin" "--ro-bind" "/sbin" "/sbin" "--ro-bind" "/sys" "/sys" ` +
		`"--ro-bind-try" "/run/dbus/system_bus_socket" "/run/dbus/system_bus_socket" ` +
		`"--ro-bind-try" "/run/systemd/system" "/run/systemd/system" "--ro-bind-try" "/etc/resolv.conf" "/etc/resolv.conf" ` +
		`"--ro-bind-try" "/etc/hosts" "/etc/hosts" "--ro-bind-try" "/etc/nsswitch.conf" "/etc/nsswitch.conf" ` +
		`"--ro-bind-try" "/etc/ssl/certs" "/etc/ssl/certs" "--ro-bind-try" "/etc/os-release" "/etc/os-release" ` +
		`"--ro-bind-try" "/etc/passwd" "/etc/passwd" "--proc" "/proc" "--dev" "/dev" "--tmpfs" "/tmp" `
	tests := []struct {
		name string
		got  []string
		want string
	}{
		{"writable workspace",
			jailArgv("/usr/bin/bwrap", "/tmp/ws/p", false, []string{"CI=true", "HOME=/home/u"}, nil, nil, "echo hi"),
			"[" + profile + `"--bind" "/tmp/ws/p" "/tmp/ws/p" "--chdir" "/tmp/ws/p" "--clearenv" ` +
				`"--setenv" "CI" "true" "--setenv" "HOME" "/tmp/ws/p" "--setenv" "PATH" "/usr/bin:/bin" "bash" "-c" "echo hi"]`},
		{"read-only workspace with a toolchain",
			jailArgv("/usr/bin/bwrap", "/tmp/ws/p", true, []string{"LANG=C"}, []string{"/opt/go"}, nil, "go test ./..."),
			"[" + profile + `"--ro-bind" "/opt/go" "/opt/go" "--dir" "/tmp/home" "--ro-bind" "/tmp/ws/p" "/tmp/ws/p" ` +
				`"--chdir" "/tmp/ws/p" "--clearenv" "--setenv" "LANG" "C" "--setenv" "PATH" "/opt/go:/usr/bin:/bin" ` +
				`"--setenv" "HOME" "/tmp/home" "bash" "-c" "go test ./..."]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmt.Sprintf("%q", tt.got); got != tt.want {
				t.Fatalf("argv changed without egress:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}

// With egress the jail keeps --unshare-all and gains only the bridge: the
// sockets, the forwarder and agent binaries read-only, the agent home as
// HOME, and proxy variables that override any the host passed through.
func TestJailArgvWithEgress(t *testing.T) {
	egress := &Egress{
		Forwarder: "/opt/agentd/agentd", ProxySocket: "/tmp/run/egress.sock", MCPSocket: "/tmp/run/mcp.sock",
		Home: "/tmp/run/home", Executable: "/opt/opencode/bin/opencode",
	}
	host := []string{"CI=true", "HTTPS_PROXY=http://host:8080", "https_proxy=http://host:8080"}
	argv := jailArgv("/usr/bin/bwrap", "/tmp/ws/p", false, host, nil, egress, "opencode acp")
	flat := strings.Join(argv, " ")
	for _, want := range []string{
		"--unshare-all",
		"--bind /tmp/run/egress.sock /run/agentd/egress.sock",
		"--bind /tmp/run/mcp.sock /run/agentd/mcp.sock",
		"--ro-bind /opt/agentd/agentd /run/agentd/agentd",
		"--ro-bind /opt/opencode/bin/opencode /opt/opencode/bin/opencode",
		"--bind /tmp/run/home /tmp/run/home",
		"--setenv HOME /tmp/run/home",
		"--setenv HTTP_PROXY http://127.0.0.1:3128",
		"--setenv NO_PROXY 127.0.0.1,localhost",
		"--setenv http_proxy http://127.0.0.1:3128",
		"--setenv no_proxy 127.0.0.1,localhost",
		"--setenv NODE_USE_ENV_PROXY 1",
		"/run/agentd/agentd jail-forward 127.0.0.1:3128=/run/agentd/egress.sock 127.0.0.1:3129=/run/agentd/mcp.sock",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("argv lacks %q:\n%s", want, flat)
		}
	}
	if strings.Contains(flat, "--share-net") || strings.Contains(flat, "--setenv HOME /tmp/ws/p") {
		t.Fatalf("argv shares the network or keeps the workspace as HOME:\n%s", flat)
	}
	for _, name := range []string{"HTTPS_PROXY", "https_proxy"} {
		last := strings.LastIndex(flat, "--setenv "+name+" ")
		if !strings.HasPrefix(flat[last:], "--setenv "+name+" http://127.0.0.1:3128") {
			t.Fatalf("the host %s wins over the jail's:\n%s", name, flat)
		}
	}
	if tmpfs, home := strings.Index(flat, "--tmpfs /tmp"), strings.Index(flat, "--bind /tmp/run/home"); tmpfs < 0 || tmpfs > home {
		t.Fatalf("the /tmp tmpfs is missing or would hide an agent home under /tmp:\n%s", flat)
	}
	if shell := argv[len(argv)-1]; !strings.HasSuffix(shell, "\nopencode acp") {
		t.Fatalf("the agent command must run after the forwarder is ready, got %q", shell)
	}
}

// cline reaches MCP over stdio, so its egress has no MCP socket to forward.
func TestJailArgvEgressWithoutMCPSocket(t *testing.T) {
	egress := &Egress{Forwarder: "/opt/agentd/agentd", ProxySocket: "/tmp/run/egress.sock"}
	flat := strings.Join(jailArgv("/usr/bin/bwrap", "/tmp/ws/p", false, nil, nil, egress, "cline"), " ")
	if strings.Contains(flat, "mcp.sock") || !strings.Contains(flat, "jail-forward 127.0.0.1:3128=/run/agentd/egress.sock)") {
		t.Fatalf("argv = %s, want only the proxy forwarded", flat)
	}
}

// Without a jail the agent would run with the host's network, the exposure
// B-086 closed, so an egress command is refused rather than run unjailed.
func TestEgressNeedsTheJail(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		probe   jailProbeResult
	}{
		{"filter-only", BackendFilterOnly, jailProbeResult{path: "/usr/bin/bwrap", userNS: true}},
		{"auto without bwrap", "", jailProbeResult{}},
		{"auto with broken userns", "", jailProbeResult{path: "/usr/bin/bwrap"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProbe(t, tt.probe, func() {
				exec := &BashExecutor{Backend: tt.backend, Egress: &Egress{ProxySocket: "/tmp/run/egress.sock"}}
				if _, _, err := exec.useJail("ws", testPayload("ws", "true")); !errors.Is(err, models.ErrSandboxUnavailable) {
					t.Fatalf("useJail() err = %v, want ErrSandboxUnavailable", err)
				}
			})
		})
	}
}
