package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Execution backends for worker commands.
const (
	// BackendAuto jails with bubblewrap where the binary and unprivileged
	// user namespaces exist, and runs filter-only elsewhere.
	BackendAuto = "auto"
	// BackendBubblewrap always jails; a missing jail still runs the command
	// filter-only, with a loud log on every such execution.
	BackendBubblewrap = "bubblewrap"
	// BackendFilterOnly never jails: the lexical filter plus the admission
	// checks are the whole sandbox. Only for platforms without bubblewrap
	// and for tests that pin the unjailed path.
	BackendFilterOnly = "filter-only"
)

// resolveBackend normalizes the configured backend; empty and unknown values
// mean auto, matching the config package's clamp-to-default style.
func resolveBackend(configured string) string {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case BackendBubblewrap:
		return BackendBubblewrap
	case BackendFilterOnly:
		return BackendFilterOnly
	default:
		return BackendAuto
	}
}

// jailProbeResult is one cached availability check: the bwrap path when the
// binary exists, and whether unprivileged user namespaces work under it.
type jailProbeResult struct {
	path   string
	userNS bool
}

// probeJailHost checks the real machine by running the same mount profile
// real executions use: the binary must exist and a jailed `true` must run
// inside it. Probing the real profile keeps the check honest — a bare
// namespace join would prove nothing about execution, and a profile that
// fails here (a missing hard bind, a kernel that refuses an unshare) makes
// auto fall back to filter-only instead of failing every Execute call.
func probeJailHost() jailProbeResult {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return jailProbeResult{}
	}
	dir, err := os.MkdirTemp("", "agentd-jail-probe-*")
	if err != nil {
		return jailProbeResult{path: path}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	argv := jailArgv(path, dir, nil, "true")
	probe := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if err := probe.Run(); err != nil {
		return jailProbeResult{path: path}
	}
	return jailProbeResult{path: path, userNS: true}
}

var probeJailOnce = sync.OnceValue(probeJailHost)

// probeJail is the cached availability check. Tests stub it via withProbe;
// production always runs the cached real probe.
var probeJail = func() jailProbeResult { return probeJailOnce() }

// useJail decides whether this execution runs under bubblewrap and warns
// loudly when an expected jail is missing. Auto stays quiet when the binary
// is absent entirely (platforms without bubblewrap must not log-scare on
// every task); a present-but-broken jail and an explicit backend warn on
// every fallback execution because the operator asked for a jail.
func (e *BashExecutor) useJail(workspace string, payload Payload) (string, bool) {
	status := probeJail()
	usable := status.path != "" && status.userNS
	switch resolveBackend(e.Backend) {
	case BackendFilterOnly:
		return "", false
	case BackendBubblewrap:
		if !usable {
			slog.Warn("sandbox: bubblewrap backend requested but unavailable; running filter-only (not a jail)",
				"workspace", workspace, "task_id", payload.TaskID, "bwrap", status.path)
		}
		return status.path, usable
	default:
		if status.path != "" && !status.userNS {
			slog.Warn("sandbox: bubblewrap user namespaces unavailable; running filter-only (not a jail)",
				"workspace", workspace, "task_id", payload.TaskID, "bwrap", status.path)
		}
		return status.path, usable
	}
}

// BackendStartupLine is the one INFO line the daemon logs at start: the backend
// it resolves to and why. It reads the same cached probe as useJail, so the
// line and the per-execution record agree.
func BackendStartupLine(configured string) string {
	backend, reason := describeBackend(configured)
	line := fmt.Sprintf("sandbox backend=%s (%s)", backend, reason)
	return line
}

// describeBackend resolves the backend for the startup line without a
// workspace. An explicit filter-only skips the probe entirely.
func describeBackend(configured string) (string, string) {
	if resolveBackend(configured) == BackendFilterOnly {
		return BackendFilterOnly, "configured filter-only"
	}
	status := probeJail()
	if status.path != "" && status.userNS {
		return BackendBubblewrap, "bwrap and userns available"
	}
	reason := unavailableReason(status)
	if resolveBackend(configured) == BackendBubblewrap {
		reason = "bubblewrap requested: " + reason + "; running filter-only"
	}
	return BackendFilterOnly, reason
}

// unavailableReason names why the jail cannot run, for the startup line.
func unavailableReason(status jailProbeResult) string {
	if status.path == "" {
		return "bwrap not found"
	}
	return "unprivileged user namespaces unavailable"
}

// jailArgv builds the full bubblewrap invocation for one workspace: a
// read-only host runtime (/usr, /lib, /bin, /sbin, /sys), the narrow dbus
// and systemd mounts hostnamectl needs to report OS facts, the /etc files
// ordinary commands need (name resolution, TLS trust, OS release, user
// lookup), a fresh proc/dev/tmp, the workspace bound writable at the same
// path with cwd inside it, and a cleared environment rebuilt from env.
// Network stays shared: --unshare-all would otherwise isolate default worker
// commands (git clone, curl, go mod download, npm install). Mounts that can
// be absent on some hosts (/lib64 on some arm64 distros, dbus/systemd on
// hosts without them, any single /etc file) use --ro-bind-try so a missing
// path degrades instead of failing the exec before bash starts. Mounts apply
// in order, so the /tmp tmpfs precedes the workspace bind — a workspace under
// /tmp would otherwise be hidden by it. Hostile tenancy stays out of scope:
// rootless podman per task is the SP-019 answer there, not a wider bind list
// here.
func jailArgv(bwrap, workspace string, env []string, shell string) []string {
	argv := []string{bwrap,
		"--unshare-all", "--share-net", "--die-with-parent",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/sbin", "/sbin",
		"--ro-bind", "/sys", "/sys",
		"--ro-bind-try", "/run/dbus/system_bus_socket", "/run/dbus/system_bus_socket",
		"--ro-bind-try", "/run/systemd/system", "/run/systemd/system",
		"--ro-bind-try", "/etc/resolv.conf", "/etc/resolv.conf",
		"--ro-bind-try", "/etc/hosts", "/etc/hosts",
		"--ro-bind-try", "/etc/nsswitch.conf", "/etc/nsswitch.conf",
		"--ro-bind-try", "/etc/ssl/certs", "/etc/ssl/certs",
		"--ro-bind-try", "/etc/os-release", "/etc/os-release",
		"--ro-bind-try", "/etc/passwd", "/etc/passwd",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--bind", workspace, workspace,
		"--chdir", workspace,
		"--clearenv",
	}
	for _, pair := range jailEnv(env, workspace) {
		key, value, _ := strings.Cut(pair, "=")
		argv = append(argv, "--setenv", key, value)
	}
	return append(argv, "bash", "-c", shell)
}

// jailEnv rebuilds the payload environment for the jail. Host PATH and HOME
// point outside the visible filesystem, so they are replaced with jail-valid
// values; every other allowlisted variable passes through unchanged.
func jailEnv(env []string, workspace string) []string {
	out := make([]string, 0, len(env)+2)
	var havePath, haveHome bool
	for _, pair := range env {
		key, _, _ := strings.Cut(pair, "=")
		switch key {
		case "PATH":
			havePath = true
			out = append(out, "PATH=/usr/bin:/bin")
		case "HOME":
			haveHome = true
			out = append(out, "HOME="+workspace)
		default:
			out = append(out, pair)
		}
	}
	if !havePath {
		out = append(out, "PATH=/usr/bin:/bin")
	}
	if !haveHome {
		out = append(out, "HOME="+workspace)
	}
	return out
}
