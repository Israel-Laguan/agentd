# E2E Journey Test Suite (T-026)

This package implements the e2e journey harness for testing agentd against a live devenv stack.

## Quick Start

```bash
# Start devenv (optional; make test-e2e does this automatically)
make dev-up

# Run all e2e tests
make test-e2e

# Run specific test with verbose output
go test -v -race -tags=e2e ./test/e2e -run TestJ01

# Stop devenv
make dev-down
```

## Journeys

The journey suite is defined in [docs/testing/journeys.md](../../docs/testing/journeys.md). Currently implemented:

- **J01**: Boot + provider connectivity (system/status envelope check; warmup
  on/off log verification is not yet automated — devenv/compose.yaml hardcodes
  `--skip-llm-warmup` into every profile's entrypoint)
- **J02**: Board and logs reachable, loop running — materializes a
  self-contained task and confirms the dispatch loop claims/runs it, and
  that an SSE event arrives on the project-scoped stream
- **J03**: Chat answers without creating a plan — a status_check intent
  returns a deterministic `status_report`, not a `DraftPlan`
- **J04**: Full happy path — chat → plan → materialize → seed workspace →
  workspace/ready → poll tasks to COMPLETED
- **J07**: Self-healing handoff (needs the "healing" profile) — a plan
  materialized against a deliberately dead connector (devenv/agentd/
  config.healing.yaml) opens the circuit breaker, producing a BLOCKED
  parent + "Manual review required:" HUMAN child; the journey resets the
  breaker first (it's process-global, not per-project — see
  APIClient.ResetBreaker's doc comment) then retries the parent

- **J08**: Unclean kill mid-task — SIGKILLs the agentd container while a
  `SLOW_TASK` (mockllm scenario) is RUNNING, restarts it on the same volume,
  and asserts the task gets a RECOVERY event. Takes ~2 min: boot reconcile
  misses the task because the daemon is PID 1 in the container (known gap,
  see docs/testing/journeys.md), so the stale-heartbeat sweep recovers it

Future journeys will test:
- J05-J06: Materialization edge cases
- J09: Provider cascade and circuit breaker
- J10: Disk space watchdog
- J11: Saved preferences recall
- J12: Tiered execution
- J13-J15: OpenAI compatibility, SSE events, MCP export

## Architecture

### Core Helpers

- **Harness**: Manages devenv profile and health checks
  - `WaitForHealthy()`: Poll /api/v1/system/status until ready
  - `Get()`: Make GET requests to the API

- **APIClient**: Convenience methods for API calls
  - `SystemStatus()`: GET /api/v1/system/status
  - `Projects()`: GET /api/v1/projects
  - `ChatCompletions()`: POST /v1/chat/completions (OpenAI-shaped; the real
    chat route — there is no `/api/v1/chat`)
  - `MaterializePlan()`: POST /api/v1/projects/materialize — the DraftPlan
    JSON from chat, unmodified. There is no separate approve endpoint or
    plan ID: materializing that exact plan IS the approval.
  - `WorkspaceReady()`: POST /api/v1/projects/{projectID}/workspace/ready,
    keyed by the project's UUID (not its name)
  - `ListTasks()`: GET /api/v1/projects/{projectID}/tasks, optionally
    filtered by state — there is no single-task GET endpoint
  - Generic `Get()`, `Post()`, `PostJSON()`, `Patch()` methods

- **SSEReader**: Consume server-sent events from /api/v1/events/stream
  (optionally scoped with `?project_id=`)
  - `NextEvent()`: Read next SSE event with timeout
  - Parses "event:" and "data:" lines

- **TaskPoller**: Poll a project's task list until desired states are reached
  - `WaitForTaskState()`: Block until one task reaches a given state
  - `WaitForAllComplete()`: Block until every task in the project is
    COMPLETED, or fail fast on FAILED/FAILED_REQUIRES_HUMAN
  - Automatic timeout protection

- **ProjectManager**: Tracks a journey's project name for isolation. There is
  no DELETE /api/v1/projects/{id} route, so `Cleanup()` doesn't exist —
  journey projects (and their workspaces) accumulate in the devenv volume
  across runs.

- **DevenvManager**: Manage devenv stack via podman-compose
  - `Start()`: Bring up this manager's profile (idempotent)
  - `Stop()`: Tear down this manager's profile
  - `WaitForReady()`: Wait for all services healthy
  - `IsRunning()`: Check if the profile has running containers
  - `SeedWorkspace()`: Create a non-empty workspace dir for a project inside
    the running agentd container via `podman compose exec` — there is no
    bind mount exposing the workspace root to the host (see
    devenv/compose.yaml's named volumes)

  All podman-compose invocations pass `--profile <profile>` explicitly:
  podman-compose 1.3.0 does not auto-activate the "default" profile the way
  docker compose does, so an unqualified `up`/`ps`/`down` silently resolves
  to `services: {}`. `NewDevenvManager` also resolves `composePath` to an
  absolute path — podman-compose 1.3.0 has been observed to fail on a
  relative `-f` path depending on how it's invoked.

### Service Profiles

Devenv supports multiple configurations via profiles in `devenv/compose.yaml`:

| Profile | Use Case | Services | Config Changes |
|---------|----------|----------|-----------------|
| default | Development | agentd, web | Standard config |
| healing | Self-healing (J07) | agentd-healing | healing.enabled: true |
| faults | Provider failures (J08-J09) | agentd-faults | gateway.order: [dead, secondary] |
| disk | Disk watchdog (J10) | agentd-disk | disk.free_threshold_percent: 100 |
| tiered | Tiered execution (J12) | agentd-tiered | tiered.enabled: true |

Each variant has:
- Separate `agentd-*` service on unique port
- Isolated database volume
- Custom config file (config.{profile}.yaml)
- mockllm and litellm shared across all profiles

### Build Tags

Tests use `//go:build e2e` to exclude them from `make test`:
- `go test ./test/e2e/...` — skips tests (no tag)
- `go test -tags=e2e ./test/e2e/...` — runs tests
- `make test` — skips e2e tests
- `make test-e2e` — runs e2e tests with dev-up

## Design Patterns

### Per-Journey Isolation

Each journey gets a unique project name (there's no cleanup step — see
ProjectManager above):
```go
name := UniqueProjectName("j01")  // "j01-abc123"
pm := NewProjectManager(client, name)
```

### Failure Diagnostics

Errors include journey ID and step for easy triage:
```
J01 [boot] harness failed to become healthy: context deadline exceeded
J04 [system/status] returned 500, want 200
```

### Profile Selection

Journeys declare their required profile:
```go
func TestJ07_HealingHandoff(t *testing.T) {
    harness := NewHarness(baseURL, "healing")
    // ... test with agentd-healing on port 8766
}
```

### State Polling with Timeout

Task state polling protects against hangs (there's no single-task GET, so
polling always goes through the project's task list):
```go
poller := NewTaskPoller(client, projectID)
tasks, err := poller.WaitForAllComplete(ctx, 60*time.Second)
if err != nil {
    t.Fatalf("tasks did not complete: %v (last observed: %+v)", err, tasks)
}
```

## Next Steps

1. **J01**: Add a devenv profile that boots without `--skip-llm-warmup` to
   automate the warmup-on/off log check
2. **Implement J05-J06**: Materialization edge cases, task drawer event log
3. **Implement J09-J12**: Remaining profile-specific journey tests (faults,
   disk, tiered)
4. **Add mock scenario injection**: Parse @scenario= tags in requests (T-028)
5. **Improve error output**: Capture last observed state for bug filing (T-027)

## Testing

Run tests with devenv already up:
```bash
make dev-up
go test -v -race -tags=e2e -timeout=300s ./test/e2e/...
make dev-down
```

Or let make handle it:
```bash
make test-e2e
```

Tests skip gracefully in short mode (`go test -short`), which is useful for CI that doesn't have container support.
