//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// profileService maps a devenv profile to its agentd compose service name
// and the container-internal home directory that service's volume mounts to
// (see devenv/compose.yaml). Workspaces live at "<home>/projects/<id>".
var profileService = map[string]struct {
	service string
	home    string
}{
	"default": {"agentd", "/home/agentd"},
	"healing": {"agentd-healing", "/home/agentd-healing"},
	"faults":  {"agentd-faults", "/home/agentd-faults"},
	"breaker": {"agentd-brk", "/home/agentd-brk"},
	"disk":    {"agentd-disk", "/home/agentd-disk"},
	"tiered":  {"agentd-tiered", "/home/agentd-tiered"},
}

// DevenvManager manages the devenv stack via podman-compose.
type DevenvManager struct {
	composePath string
	profile     string
}

// NewDevenvManager creates a manager for the devenv stack.
// The profile must exist in the compose file (default, healing, faults, disk, tiered).
// composePath is resolved to an absolute path: podman-compose 1.3.0 has been
// observed to os.chdir() before re-opening a relative -f path, so a relative
// path silently fails with "No such file or directory" depending on how it
// was invoked.
func NewDevenvManager(composePath, profile string) *DevenvManager {
	if abs, err := filepath.Abs(composePath); err == nil {
		composePath = abs
	}
	return &DevenvManager{
		composePath: composePath,
		profile:     profile,
	}
}

// IsRunning checks if this manager's profile has any running containers.
// podman-compose 1.3.0 does not auto-activate the "default" profile the way
// docker compose does (an unqualified `ps`/`up` sees `services: {}`), so
// every compose invocation here must pass --profile explicitly.
func (m *DevenvManager) IsRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath,
		"--profile", m.profile, "ps", "-q")
	output, err := cmd.Output()
	return err == nil && len(bytes.TrimSpace(output)) > 0
}

// Start brings up this manager's profile. If it's already running, this is
// a no-op.
func (m *DevenvManager) Start(ctx context.Context) error {
	if m.IsRunning(ctx) {
		return nil // Already running.
	}

	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath,
		"--profile", m.profile, "up", "--build", "-d")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start devenv: %w (output: %s)", err, string(output))
	}
	return nil
}

// Stop brings down this manager's profile.
func (m *DevenvManager) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath,
		"--profile", m.profile, "down")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop devenv: %w (output: %s)", err, string(output))
	}
	return nil
}

// WaitForReady waits for the devenv stack to be healthy.
func (m *DevenvManager) WaitForReady(ctx context.Context, timeout time.Duration) error {
	harness := NewHarness("http://localhost:8765", m.profile)
	return harness.WaitForHealthy(ctx, timeout)
}

// KillAgentd SIGKILLs this profile's agentd container (an unclean crash: no
// graceful shutdown, no deferred cleanup) without removing it, so its data
// volume — and therefore its SQLite board — survives for RestartAgentd.
func (m *DevenvManager) KillAgentd(ctx context.Context) error {
	return m.serviceCmd(ctx, "kill")
}

// RestartAgentd starts this profile's previously killed agentd container on
// the same data volume.
func (m *DevenvManager) RestartAgentd(ctx context.Context) error {
	return m.serviceCmd(ctx, "start")
}

func (m *DevenvManager) serviceCmd(ctx context.Context, verb string) error {
	svc, ok := profileService[m.profile]
	if !ok {
		return fmt.Errorf("no known agentd service for profile %q", m.profile)
	}
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath,
		"--profile", m.profile, verb, svc.service)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w (output: %s)", verb, svc.service, err, string(output))
	}
	return nil
}

// SeedWorkspace creates a non-empty workspace directory for projectID inside
// the running agentd container for this manager's profile, by shelling into
// the container via `podman compose exec` (there is no bind mount exposing
// the workspace root to the host — see devenv/compose.yaml's named volumes).
// This mirrors docs/demo.md's manual seeding step so WorkspaceReady can
// unlock PENDING tasks without relying on DraftPlan.StartEmptyWorkspace.
func (m *DevenvManager) SeedWorkspace(ctx context.Context, projectID string) error {
	svc, ok := profileService[m.profile]
	if !ok {
		return fmt.Errorf("no known agentd service for profile %q", m.profile)
	}
	dir := svc.home + "/projects/" + projectID
	shellCmd := fmt.Sprintf("mkdir -p %s && echo seed > %s/README.md", dir, dir)
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath,
		"--profile", m.profile, "exec", "-T", svc.service, "sh", "-c", shellCmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("seed workspace for project %s: %w (output: %s)", projectID, err, string(output))
	}
	return nil
}
