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
- **J09**: Provider cascade and circuit breaker, split across two profiles
  because they need opposite configs. `TestJ09_ProviderCascade` (faults
  profile) asserts a request succeeds despite a dead first-choice provider
  and the breaker stays CLOSED; `TestJ09_BreakerOpens` (breaker profile,
  every provider unreachable) asserts the breaker reaches OPEN and the task
  gains a "Manual review required:" HUMAN child. ProviderUsed has no HTTP
  surface, so the cascade is asserted behaviourally
- **J10**: Disk space watchdog (disk profile) — asserts exactly one HUMAN
  "Disk space critical" task in `_system` with exactly one
  `DISK_SPACE_CRITICAL` event, still exactly one after several further
  watchdog passes. The threshold is 100%, so the watchdog always fires on
  the container's overlay fs; the journey tests dedup, not a real
  disk-full condition
- **J11**: Saved preference recall — a preference saved via
  `POST /api/v1/preferences` must reach a *later* task's execution prompt.
  Three phases: absent before the save (baseline), present after, and
  absent for an unrelated user (so a leak into every prompt would fail).
  Observes the real prompt via the mock's request capture — see
  `MockLLMClient` below
- **J14**: SSE event delivery — opens a project-scoped stream, runs a task
  READY → COMPLETED, and asserts the lifecycle signals arrive in causal
  order (LOG_CHUNK before RESULT), then reconciles the live stream against
  the task's durable event log. Note the spec's expected `task-started` /
  `task-claimed` / `task-completed` events do not exist: claim and start
  write no event row at all, so there is nothing to stream for them

Deferred (P1/P2, see docs/testing/journeys.md for the policy):
- J05-J06: Materialization edge cases, task drawer event log
- J12: Tiered execution
- J13: OpenAI compatibility, J15: MCP export

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
  - `DrainEvents()`: Read until the stream goes quiet, returning everything in
    arrival order — suits assertions over a burst and its ordering rather than
    a single event
  - Parses "event:" and "data:" lines

- **MockLLMClient**: Read back what the worker actually sent to the model
  (`GET /requests` on the devenv mock, which appends every request body to a
  JSONL log). This is the only channel for asserting on prompt contents —
  nothing in the agentd API exposes them, which is how the older
  `scripts/demo/memory-recall.sh` ended up simulating its own success.
  `WorkerPrompts(taskID)` attributes captured prompts to a task via the
  request's `agentd_metadata.task_id`

- **TaskPoller**: Poll a project's task list until desired states are reached
  - `WaitForTaskState()`: Block until one task reaches a given state
  - `WaitForAllComplete()`: Block until every task in the project is
    COMPLETED, or fail fast on FAILED/FAILED_REQUIRES_HUMAN
  - `WaitForHumanHandoff()` / `PollHumanHandoff()`: Look for a HUMAN-assigned
    task (self-healing handoffs are filtered out of default listings, so
    these pass `include_healing=true`). The single-shot `PollHumanHandoff`
    exists so a caller can interleave other work — J09 releases the breaker's
    probe gate between polls
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
| faults | Provider cascade (J09-A) | agentd-faults | gateway.order: [dead, secondary] |
| breaker | Breaker trip (J09-B) | agentd-brk | gateway.order: [dead, dead2] (all dead) |
| disk | Disk watchdog (J10) | agentd-disk | disk.free_threshold_percent: 100 + a crontab with `@every 5s disk-watchdog` |
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

## Bringing up the non-default profiles

`make test-e2e` only starts the `default` profile. Journeys on other profiles
need them started explicitly, and **all profiles must be named in a single
invocation** — podman-compose 1.3.0 resolves `depends_on` only within the
activated profiles, so `--profile breaker up -d` alone fails with
`KeyError: 'litellm'`:

```bash
podman compose -f "$PWD/devenv/compose.yaml" \
  --profile default --profile healing --profile faults \
  --profile breaker --profile disk up -d
```

The mock LLM is published on `127.0.0.1:8000` for J11's request capture.

## Next Steps

1. **J01**: Add a devenv profile that boots without `--skip-llm-warmup` to
   automate the warmup-on/off log check
2. **Implement J05-J06**: Materialization edge cases, task drawer event log
3. **Implement J12-J13, J15**: Tiered execution, OpenAI compatibility, MCP export
4. **Add mock scenario injection**: Parse @scenario= tags in requests (T-028)
5. **Tighten J08** once boot reconcile stops skipping PID-1 tasks
6. **T-027**: run every P0 journey twice on a clean stack and triage

All ten P0 journeys (J01-J04, J07-J11, J14) pass. See
[docs/testing/journeys.md](../../docs/testing/journeys.md) for the spec, the
bugs the journeys found, and the deferral policy for P1/P2.

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
