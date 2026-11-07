# B-061 (rest): capability routing routes nothing (the registry is never set)

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog (planned in agentd, never executed; moved out with the harness on 2026-11-05) |
| Priority | P3 |
| Sprint | — (agentd S15 fixed the batching side; real routing deferred) |
| Links | agentd S15 B-061, `queue/worker/worker_tools_capability.go`, `capabilities`, B-063 (rest) |

## Symptoms

The capability adapter loads and matches tasks, but nothing populates the worker's capability registry at daemon startup, so external capability routing never fires (historical harness source: `queue/worker/worker_tools_capability.go` checked `TryExternalCapabilityRoute` against an unset registry and logged "no capability registry; falling through to agentic loop"). agentd S15 made batching ignore routes that can never fire; the routing itself was never built.

## What is left

- Construct the registry in the daemon from mounted plugins (needs B-063 rest) or another capability source.
- A test with a registered capability and a matching task: the task runs on the capability, not the loop.
