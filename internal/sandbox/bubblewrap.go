package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agentd/internal/models"
)

// Execution backends for worker commands.
const (
	// BackendAuto jails with bubblewrap where the binary and unprivileged
	// user namespaces exist, and runs filter-only elsewhere.
	BackendAuto = "auto"
	// BackendBubblewrap requires the jail: when it is unusable, commands are
	// refused with models.ErrSandboxUnavailable and never run unjailed.
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

// ValidateBackend rejects a configured backend that resolveBackend would
// silently turn into auto. Empty is valid and means auto.
func ValidateBackend(configured string) error {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "", BackendAuto, BackendBubblewrap, BackendFilterOnly:
		return nil
	}
	return fmt.Errorf("unknown backend %q (valid: %s, %s, %s)", configured, BackendAuto, BackendBubblewrap, BackendFilterOnly)
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
	argv := jailArgv(path, dir, nil, nil, "true")
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

// useJail decides whether this execution runs under bubblewrap. Auto falls back
// to filter-only: quiet when the binary is absent entirely (platforms without
// bubblewrap must not log-scare on every task), and warned when bwrap is present
// but unprivileged user namespaces are not. An explicit bubblewrap is a
// requirement: an unusable jail returns ErrSandboxUnavailable, so the command is
// refused before any shell starts and the error names the reason.
func (e *BashExecutor) useJail(workspace string, payload Payload) (string, bool, error) {
	status := probeJail()
	switch resolveBackend(e.Backend) {
	case BackendFilterOnly:
		return "", false, nil
	case BackendBubblewrap:
		if !status.usable() {
			slog.Error("sandbox: bubblewrap backend required but unavailable; command refused",
				"workspace", workspace, "task_id", payload.TaskID, "bwrap", status.path)
			return "", false, fmt.Errorf("%w: sandbox.backend=bubblewrap is required, but %s; command refused",
				models.ErrSandboxUnavailable, unavailableReason(status))
		}
		return status.path, true, nil
	default:
		if status.path != "" && !status.userNS {
			slog.Warn("sandbox: bubblewrap user namespaces unavailable; running filter-only (not a jail)",
				"workspace", workspace, "task_id", payload.TaskID, "bwrap", status.path)
		}
		return status.path, status.usable(), nil
	}
}

// BackendStartupLine is the one line the daemon logs at start: the backend that
// will run commands and why. It reads the same cached probe as useJail, so the
// line and the per-execution record agree.
func BackendStartupLine(configured string) string {
	if resolveBackend(configured) == BackendFilterOnly {
		return "sandbox backend=filter-only (configured filter-only)"
	}
	status := probeJail()
	if status.usable() {
		return "sandbox backend=bubblewrap (bwrap and userns available)"
	}
	if resolveBackend(configured) == BackendBubblewrap {
		return fmt.Sprintf("sandbox backend=bubblewrap REQUIRED but unavailable (%s); commands will be refused", unavailableReason(status))
	}
	return fmt.Sprintf("sandbox backend=filter-only (%s)", unavailableReason(status))
}

// BackendStartupLevel is the level for BackendStartupLine. An explicit bubblewrap
// that is required but unavailable is ERROR: every command will be refused. Auto's
// fallback is a configured outcome, so it stays INFO.
func BackendStartupLevel(configured string) slog.Level {
	if resolveBackend(configured) == BackendBubblewrap && !probeJail().usable() {
		return slog.LevelError
	}
	return slog.LevelInfo
}

// usable reports whether bubblewrap can run commands: the binary exists and
// unprivileged user namespaces work under it.
func (status jailProbeResult) usable() bool {
	if status.path == "" {
		return false
	}
	return status.userNS
}

// unavailableReason names why the jail cannot run, for the startup line and the
// refusal error.
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
// --unshare-all includes the network (B-086): a jailed command gets only its
// own private loopback, never the host's (the daemon's board API listens
// there) or the internet. Model calls run in the daemon, not the jail, so no
// agentic command needs the network; dependency downloads (go mod download,
// npm install) need a vendored tree or a warm cache until an egress allowlist
// exists (see docs/agentic-harness-roadmap.md). Mounts that can
// be absent on some hosts (/lib64 on some arm64 distros, dbus/systemd on
// hosts without them, any single /etc file) use --ro-bind-try so a missing
// path degrades instead of failing the exec before bash starts. Mounts apply
// in order, so the /tmp tmpfs precedes the workspace bind — a workspace under
// /tmp would otherwise be hidden by it. Hostile tenancy stays out of scope:
// rootless podman per task is the SP-019 answer there, not a wider bind list
// here. Configured toolchain directories (sandbox.toolchain_paths, B-081) are
// bound read-only after the /tmp tmpfs, for the same reason as the workspace.
func jailArgv(bwrap, workspace string, env, toolchains []string, shell string) []string {
	argv := []string{bwrap,
		"--unshare-all", "--die-with-parent",
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
	}
	for _, dir := range toolchains {
		argv = append(argv, "--ro-bind", dir, dir)
	}
	argv = append(argv,
		"--bind", workspace, workspace,
		"--chdir", workspace,
		"--clearenv",
	)
	for _, pair := range jailEnv(env, workspace, toolchainPathEntries(toolchains)) {
		key, value, _ := strings.Cut(pair, "=")
		argv = append(argv, "--setenv", key, value)
	}
	return append(argv, "bash", "-c", shell)
}

// jailEnv rebuilds the payload environment for the jail. Host PATH and HOME
// point outside the visible filesystem, so they are replaced with jail-valid
// values; every other allowlisted variable passes through unchanged. Toolchain
// bin directories come first on PATH.
func jailEnv(env []string, workspace string, toolchainBins []string) []string {
	out := make([]string, 0, len(env)+2)
	jailPath := "PATH=" + strings.Join(append(append([]string(nil), toolchainBins...), "/usr/bin", "/bin"), ":")
	var havePath, haveHome bool
	for _, pair := range env {
		key, _, _ := strings.Cut(pair, "=")
		switch key {
		case "PATH":
			havePath = true
			out = append(out, jailPath)
		case "HOME":
			haveHome = true
			out = append(out, "HOME="+workspace)
		default:
			out = append(out, pair)
		}
	}
	if !havePath {
		out = append(out, jailPath)
	}
	if !haveHome {
		out = append(out, "HOME="+workspace)
	}
	return out
}

// toolchainPathEntries maps each toolchain directory to the PATH entry that
// exposes it: its bin subdirectory when one exists, else the directory itself.
func toolchainPathEntries(toolchains []string) []string {
	entries := make([]string, 0, len(toolchains))
	for _, dir := range toolchains {
		bin := filepath.Join(dir, "bin")
		if info, err := os.Stat(bin); err == nil && info.IsDir() {
			entries = append(entries, bin)
			continue
		}
		entries = append(entries, dir)
	}
	return entries
}
