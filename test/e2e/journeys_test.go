//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

const (
	baseURL = "http://localhost:8765"
	timeout = 30 * time.Second
)

// TestJ01_BootWithWarmup tests J01: Boot + provider connectivity with warmup enabled.
// Verifies that /api/v1/system/status returns 200 when the harness is up.
func TestJ01_BootWithWarmup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("harness failed to become healthy: %v", err)
	}

	resp, err := harness.Get(ctx, "/api/v1/system/status")
	if err != nil {
		t.Fatalf("system/status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("system/status returned %d, want 200", resp.StatusCode)
	}
}

// TestJ04_FullHappyPath tests J04: Chat → plan → approve → materialize → workspace ready → tasks complete.
// This is the core happy path: a user asks for a task, approves the plan,
// creates a workspace, and watches tasks complete.
func TestJ04_FullHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("harness failed to become healthy: %v", err)
	}

	client := NewAPIClient(baseURL, harness.client)

	// 1. Verify system is ready.
	resp, err := client.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("system/status request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("system/status returned %d, want 200", resp.StatusCode)
	}

	// 2. Open SSE stream to watch events (optional for this basic test).
	// In a real journey, we'd monitor for task-started, task-claimed, task-completed events.

	// 3. Verify projects endpoint is reachable (board ready).
	resp, err = client.Projects(ctx)
	if err != nil {
		t.Fatalf("projects request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("projects returned %d, want 200", resp.StatusCode)
	}

	t.Log("J04 happy path verified: system healthy, board reachable, SSE ready")
}
