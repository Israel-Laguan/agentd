# T-022: Debug-on-demand leveled logging at key boundaries

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S06-stabilize-observe |
| Links | [US-006](../stories/US-006-pipeline-debug-observability.md) |

## Goal

Add `slog.Debug` calls at key subsystem boundaries so a maintainer can flip `log_level: debug` (or `-v`) to trace a request through intake → router → gateway → worker loop, then turn it off once the issue is resolved.

Normal mode emits only errors and warnings with enough context to point at the right subsystem. No correlation ID plumbing, no always-on structured telemetry, no new dependencies.

## Acceptance criteria

- [ ] Chat intake logs request flow and completion/error outcome at debug level
- [ ] Router logs provider/model selection, cascade attempts, and errors at debug level
- [ ] Provider adapter logs endpoint, response status, latency, and token usage at debug level
- [ ] Agentic loop logs task ID, turn lifecycle, tool dispatch, and terminal result at debug level
- [ ] Error-level logs include enough context (subsystem, task ID, provider) to diagnose without enabling debug
- [ ] Debug logging is silent by default; enabled via existing `log_level` config or `-v` flag
- [ ] No API keys, auth headers, or prompt contents are emitted at any level
