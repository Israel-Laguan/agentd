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

- **J01**: Boot + provider connectivity (warmup on/off)
- **J04**: Full happy path (system status, board, SSE)

Future journeys will test:
- J02: Board and logs reachable
- J03: Chat answers without plan
- J05-J06: Materialization edge cases
- J07: Self-healing handoff
- J08-J09: Provider failures and circuit breaker
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
  - `Chat()`: POST /api/v1/chat
  - `MaterializePlan()`: POST to materialize a plan
  - `WorkspaceReady()`: POST workspace/ready
  - Generic `Get()`, `Post()`, `Patch()` methods

- **SSEReader**: Consume server-sent events from /api/v1/sse
  - `NextEvent()`: Read next SSE event with timeout
  - Parses "event:" and "data:" lines

- **TaskPoller**: Poll task state until desired state reached
  - `WaitForState()`: Block until task reaches READY, RUNNING, COMPLETED, etc.
  - `CurrentState()`: Get task state immediately
  - Automatic timeout protection

- **ProjectManager**: Per-journey project isolation
  - `Create()`: Create project (auto-created on first materialize)
  - `Cleanup()`: Delete project after journey
  - Isolates journeys via unique names (j01-abc123 format)

- **DevenvManager**: Manage devenv stack via podman-compose
  - `Start()`: Bring up devenv (idempotent)
  - `Stop()`: Tear down devenv
  - `WaitForReady()`: Wait for all services healthy
  - `IsRunning()`: Check if running

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

Each journey gets a unique project name:
```go
name := UniqueProjectName("j01")  // "j01-abc123"
pm := NewProjectManager(client, name)
defer pm.Cleanup(ctx)
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

Task state polling protects against hangs:
```go
poller := NewTaskPoller(client, "myproject", "task-123")
task, err := poller.WaitForState(ctx, TaskStateCompleted, 30*time.Second)
if err != nil {
    t.Fatalf("Task stuck in %s: %v", task.State, err)
}
```

## Next Steps

1. **Enhance J01**: Verify warmup logs when gateway.warmup_enabled: true
2. **Enhance J04**: Add chat request, plan approval, workspace creation, task completion
3. **Implement J02-J03**: Board and logs, chat-only response
4. **Implement J07-J12**: Profile-specific journey tests
5. **Add mock scenario injection**: Parse @scenario= tags in requests (T-028)
6. **Improve error output**: Capture last observed state for bug filing (T-027)

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
