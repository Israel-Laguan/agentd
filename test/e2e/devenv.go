//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// DevenvManager manages the devenv stack via podman-compose.
type DevenvManager struct {
	composePath string
	profile     string
}

// NewDevenvManager creates a manager for the devenv stack.
// The profile must exist in the compose file (default, healing, faults, disk, tiered).
func NewDevenvManager(composePath, profile string) *DevenvManager {
	return &DevenvManager{
		composePath: composePath,
		profile:     profile,
	}
}

// IsRunning checks if the devenv stack is currently running.
func (m *DevenvManager) IsRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath, "ps")
	err := cmd.Run()
	return err == nil
}

// Start brings up the devenv stack (or profile if specified).
// If the stack is already running, this is a no-op.
func (m *DevenvManager) Start(ctx context.Context) error {
	if m.IsRunning(ctx) {
		return nil // Already running.
	}

	// podman-compose up -d [services...]
	// For now, always bring up the full default stack.
	// Profile-specific services will be handled later.
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath,
		"up", "--build", "-d")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start devenv: %w (output: %s)", err, string(output))
	}
	return nil
}

// Stop brings down the devenv stack.
func (m *DevenvManager) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", m.composePath, "down")
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
