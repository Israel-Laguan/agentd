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
- **J05**: Materialization edge cases — a plan with no workspace intent comes
  back 201/PENDING and `workspace/ready` 409s until the workspace is seeded
  (the spec's "materialize returns 409" step described an API that does not
  exist); double-ready is idempotent 200; a bad `source_path` (missing path,
  or a file rather than a directory) is a 400 with no project row left behind
- **J13**: OpenAI-compatible intake — the `chat.completion` envelope, the
  `tools` / `tool_choice` opt-in (`tool_calls` only for a tool the client
  declared, suppressed by `tool_choice: "none"`), the `stream: true` chunk
  framing with its `[DONE]` sentinel, and the 400 error intake. No `usage`
  block is required: it is `omitempty` and never populated
- **J15**: MCP board export — `tools/list` advertises the eight documented
  board tools, and `board.list_projects` / `board.list_tasks` /
  `board.get_task` return a just-materialized project. The transport is
  JSON-RPC over Streamable HTTP at `POST /mcp`, **not** a REST export route,
  and every tool payload is a JSON string in `content[0].text`. Requires
  `mcp.enabled: true` (set in devenv/agentd/config.yaml; the product default
  is off). Two open contract limits are documented in
  docs/mcp-board-export.md and filed as B-005 (silent 100-task cap) and B-006
  (`state` ignored when `project_id` is passed)

Deferred (see docs/testing/journeys.md for the policy and re-entry conditions):
- **J06**: task drawer event log — needs a browser (UI journeys move to
  Phase 2), and two of its three expected events do not exist at all
- **J12**: tiered execution — blocked on T-028 per-request mock scenarios and
  tiered verify replies; the `tiered` profile fixture already exists

## Architecture

### Core Helpers

- **Harness**: Manages devenv profile and health checks
  - `WaitForHealthy()`: Poll /api/v1/system/status until ready
  - `Get()`: Make GET requests to the API

- **APIClient**: Convenience methods for API calls
  - `PostMCP()`: POST /mcp with the Accept header the Streamable HTTP
    transport requires (`application/json, text/event-stream`). Go's
    http.Client sends no Accept by default and is answered 400; curl's `*/*`
    passes. Every MCP call goes through this
  - `SystemStatus()`: GET /api/v1/system/status
  - `Projects()`: GET /api/v1/projects
  - `ChatCompletions()`: POST /v1/chat/completions (OpenAI-shaped; the real
    chat route — there is no `/api/v1/chat`)
  - `MaterializePlan()`: POST /api/v1/projects/materialize — the DraftPlan
    JSON from chat, unmodified. There is no separate approve endpoint or
    plan ID: materializing that exact plan IS the approval. A bad
    `source_path` is rejected 400 before anything is persisted
  - `CallMCPTool()` / `ListMCPTools()`: JSON-RPC calls to /mcp, decoding the
    SSE-framed response and the `content[0].text` JSON payload
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
  nothing in the agentd API exposes them, which is how the old memory-recall
  demo script (deleted in T-025) ended up simulating its own success.
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
  - `CreateSourceDir()` / `CreateSourceFile()`: Stage a `source_path` inside
    the agentd container and return its container-internal path. The daemon
    reads the path itself, so a host temp dir would not resolve

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
| tiered | Tiered execution (J12, deferred) | agentd-tiered | tiered.enabled: true |

The `default` profile also sets `mcp.enabled: true` / `transport: http` so
J15 can reach `/mcp`; the product default is off.

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
2. **J06**: needs a browser test tier; also blocked on the task-lifecycle
   events it expects actually existing
3. **J12**: blocked on T-028 (per-request `@scenario=` selection and tiered
   verify replies)
4. **Add mock scenario injection**: Parse @scenario= tags in requests (T-028)
5. **Tighten J08** once boot reconcile stops skipping PID-1 tasks

All ten P0 journeys (J01-J04, J07-J11, J14) and the three implemented P1
journeys (J05, J13, J15) pass. See
[docs/testing/journeys.md](../../docs/testing/journeys.md) for the spec, the
bugs the journeys found, and the deferral policy for the rest.

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
