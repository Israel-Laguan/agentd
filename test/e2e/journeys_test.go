//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

const (
	baseURL     = "http://localhost:8765"
	timeout     = 30 * time.Second
	composePath = "../../devenv/compose.yaml"
)

// TestJ01_BootWithWarmup tests J01: Boot + provider connectivity.
// Step 1: Start agentd (devenv brings it up with --skip-llm-warmup, per
// devenv/compose.yaml's entrypoint).
// Step 2: Request /api/v1/system/status returns 200 with the expected shape.
//
// Full warmup on/off log verification (steps 1-2, 4-5 of the journey spec)
// needs a second devenv profile that boots without --skip-llm-warmup, which
// doesn't exist yet — devenv/compose.yaml hardcodes the flag into every
// profile's entrypoint. Left for a follow-up devenv change; see
// docs/testing/journeys.md J01.
func TestJ01_BootWithWarmup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	harness := NewHarness(baseURL, "default")

	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J01 [boot] harness failed to become healthy: %v", err)
	}

	resp, err := harness.Get(ctx, "/api/v1/system/status")
	if err != nil {
		t.Fatalf("J01 [system/status] request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J01 [system/status] returned %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("J01 [system/status] decode failed: %v", err)
	}
	if envelope.Status != "success" {
		t.Fatalf("J01 [system/status] status = %q, want %q", envelope.Status, "success")
	}

	t.Log("J01: Boot verified - system/status 200 OK with success envelope")
}

// TestJ04_FullHappyPath tests J04: Chat → plan → materialize → workspace
// ready → tasks complete.
//
// There is no separate "approve" endpoint in the real API: the DraftPlan
// JSON returned by /v1/chat/completions IS the request body for
// POST /api/v1/projects/materialize (see test/e2e/project.go's DraftPlan
// doc comment). "Approval" is simply the caller choosing to materialize
// that exact plan.
func TestJ04_FullHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J04 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)

	j04VerifySystemReady(ctx, t, client)
	plan := j04RequestPlan(ctx, t, client)
	projectID := j04Materialize(ctx, t, client, plan)
	j04UnlockWorkspace(ctx, t, client, projectID)
	j04AwaitCompletion(ctx, t, client, projectID)
}

// j04VerifySystemReady covers step 1: system is ready, board is reachable.
func j04VerifySystemReady(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	resp, err := client.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("J04 [system/status] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J04 [system/status] returned %d, want 200", resp.StatusCode)
	}

	resp, err = client.Projects(ctx)
	if err != nil {
		t.Fatalf("J04 [projects] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J04 [projects] returned %d, want 200", resp.StatusCode)
	}
}

// j04RequestPlan covers step 2: chat asks for a small, single-scope build;
// mockllm (see devenv/mockllm/server.py) returns a DraftPlan JSON in the
// assistant message content for any message matching its PLAN_KEYWORDS.
func j04RequestPlan(ctx context.Context, t *testing.T, client *APIClient) DraftPlan {
	t.Helper()

	projectName := UniqueProjectName("j04")
	message := fmt.Sprintf("Create a project named %s that writes hello.txt to the workspace.", projectName)
	resp, err := client.ChatCompletions(ctx, message, nil)
	if err != nil {
		t.Fatalf("J04 [chat] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J04 [chat] returned %d, want 200", resp.StatusCode)
	}
	chatResp, err := DecodeChatCompletion(resp)
	if err != nil {
		t.Fatalf("J04 [chat] decode failed: %v", err)
	}
	if len(chatResp.Choices) == 0 {
		t.Fatalf("J04 [chat] response had no choices")
	}
	content := chatResp.Choices[0].Message.Content

	var plan DraftPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		t.Fatalf("J04 [chat] could not parse assistant content as a plan: %v (content: %s)", err, content)
	}
	if len(plan.Tasks) == 0 {
		t.Fatalf("J04 [chat] response was not a plan (no tasks); content: %s", content)
	}
	// Override the mock-derived name with our isolated, unique one before
	// materializing — the mock only loosely regexes a name out of the
	// message text.
	plan.ProjectName = projectName
	return plan
}

// j04Materialize covers step 3: materialize == approve. No
// source_path/start_empty_workspace, so root tasks are created PENDING and
// require an explicit WorkspaceReady call (see
// internal/services/project_service.go). Returns the new project's UUID.
func j04Materialize(ctx context.Context, t *testing.T, client *APIClient, plan DraftPlan) string {
	t.Helper()

	resp, err := client.MaterializePlan(ctx, plan)
	if err != nil {
		t.Fatalf("J04 [materialize] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		t.Fatalf("J04 [materialize] returned %d, want 201", resp.StatusCode)
	}
	materialized, err := DecodeMaterializeResult(resp)
	if err != nil {
		t.Fatalf("J04 [materialize] decode failed: %v", err)
	}
	if materialized.Project.ID == "" {
		t.Fatalf("J04 [materialize] response had no project ID")
	}
	if len(materialized.Tasks) == 0 {
		t.Fatalf("J04 [materialize] response had no tasks")
	}
	for _, task := range materialized.Tasks {
		if task.State != TaskStatePending {
			t.Fatalf("J04 [materialize] task %s state = %s, want PENDING (workspace not yet seeded)", task.ID, task.State)
		}
	}
	return materialized.Project.ID
}

// j04UnlockWorkspace covers step 4: seed the workspace (docs/demo.md's
// manual step, automated via `podman compose exec`, since the workspace
// root isn't bind-mounted to the host) then mark it ready to unlock the
// PENDING root tasks.
func j04UnlockWorkspace(ctx context.Context, t *testing.T, client *APIClient, projectID string) {
	t.Helper()

	devenv := NewDevenvManager(composePath, "default")
	if err := devenv.SeedWorkspace(ctx, projectID); err != nil {
		t.Fatalf("J04 [seed workspace] failed: %v", err)
	}

	resp, err := client.WorkspaceReady(ctx, projectID)
	if err != nil {
		t.Fatalf("J04 [workspace/ready] request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J04 [workspace/ready] returned %d, want 200", resp.StatusCode)
	}
}

// j04AwaitCompletion covers step 5: watch the board until every task
// completes.
func j04AwaitCompletion(ctx context.Context, t *testing.T, client *APIClient, projectID string) {
	t.Helper()

	poller := NewTaskPoller(client, projectID)
	finalTasks, err := poller.WaitForAllComplete(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("J04 [tasks] did not complete: %v (last observed: %+v)", err, finalTasks)
	}

	t.Logf("J04: Happy path verified - project %s materialized %d task(s), all COMPLETED", projectID, len(finalTasks))
}
