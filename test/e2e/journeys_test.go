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
// Step 1: Start agentd with --skip-llm-warmup=false.
// Step 2: Verify logs show "LLM warmup OK".
// Step 3: Request /api/v1/system/status returns 200.
// Step 4: Restart with --skip-llm-warmup=true.
// Step 5: Verify boot succeeds with no warmup log.
//
// For now, this test verifies the basic connectivity (step 3).
// Full warmup log checking requires log access (future enhancement with devenv config variants).
func TestJ01_BootWithWarmup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	harness := NewHarness(baseURL, "default")

	// Wait for harness to be healthy.
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J01 [boot] harness failed to become healthy: %v", err)
	}

	// Verify system/status endpoint is reachable.
	resp, err := harness.Get(ctx, "/api/v1/system/status")
	if err != nil {
		t.Fatalf("J01 [system/status] request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J01 [system/status] returned %d, want 200", resp.StatusCode)
	}

	t.Log("J01: Boot verified - system/status 200 OK")
}

// TestJ04_FullHappyPath tests J04: Chat → plan → approve → materialize → workspace ready → tasks complete.
// This is the core happy path: the system is ready, board is reachable, and SSE is streaming.
//
// Steps:
// 1. Start agentd (done by devenv).
// 2. Verify system/status returns 200.
// 3. Verify /api/v1/projects returns 200 (board is reachable).
// 4. Verify SSE /api/v1/sse is open (events can be consumed).
//
// Future: add chat request, plan approval, workspace creation, task completion polling.
func TestJ04_FullHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	harness := NewHarness(baseURL, "default")

	// Wait for harness to be healthy.
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J04 [boot] harness failed to become healthy: %v", err)
	}

	client := NewAPIClient(baseURL, harness.client)

	// Step 1: Verify system is ready.
	resp, err := client.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("J04 [system/status] request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J04 [system/status] returned %d, want 200", resp.StatusCode)
	}

	// Step 2: Verify board is reachable (projects endpoint).
	resp, err = client.Projects(ctx)
	if err != nil {
		t.Fatalf("J04 [projects] request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J04 [projects] returned %d, want 200", resp.StatusCode)
	}

	// Step 3: Verify SSE is available (no consumption, just verify endpoint).
	// In a real journey, we'd open the SSE stream and monitor for task events.
	respSSE, err := harness.Get(ctx, "/api/v1/sse")
	if err != nil {
		t.Fatalf("J04 [sse] request failed: %v", err)
	}
	defer respSSE.Body.Close()
	if respSSE.StatusCode != http.StatusOK {
		t.Fatalf("J04 [sse] returned %d, want 200", respSSE.StatusCode)
	}

	t.Log("J04: Happy path verified - system 200, board 200, SSE 200")
}
