# agentd Test Execution Results

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Historical run outcomes and
> known gaps confirmed live. Append new runs here rather than growing the plan.

## Execution results and notes

### CP0 verification

- Isolated warmup run succeeded: `tool credentials validated` → `running LLM warmup` → `LLM warmup OK` → `API server listening` → `starting daemon`.

- Warmup failure run confirmed hard exit (code 1) with `command failed` summary when litellm was stopped.

- Compose default (`--skip-llm-warmup`) boots to `API server listening` with **no** warmup lines, as expected.

### CP3b edge cases

- `workspace/ready` before seeding returned `409` / `ErrWorkspaceNotReady` with tasks staying `PENDING`.

- Nonexistent `source_path` materialization returned an error. `MaterializePlan` persists the
  project row before seeding, so the seeded-state claim is not established by this run: check
  `GET /api/v1/projects` (or fetch the project by ID) to see whether the row remains, and treat
  the error as a seed failure rather than an atomic rollback.

- `workspace/ready` twice after a valid seed was safe (second call succeeded without duplicating dispatch).

### Chat-to-Kanban QA script (historical — script deleted 2026-09-30, superseded by J04 and J07)

- `./test/e2e/chat-kanban.sh` created a project, materialized it with `start_empty_workspace: true`, and tasks reached `COMPLETED`.

- The script then timed out waiting for a human-attention task. This is expected with `healing.enabled: false` in `devenv/compose.yaml`; no handoff/attention flow was triggered. Do not treat this as a script failure unless a real-provider run with healing enabled is intended.

- Project/task IDs for investigation: `ca0cdbd4-c56a-427e-9c3e-794fcfd13acd`.

- The stack now uses LiteLLM with model `agentd` (previously `mock/agentd`). The
  `/v1/models` endpoint returns `agentd` and agentd's provider config references
  `model: "agentd"`.

### Known gaps confirmed live

- Web Logs panel always shows `System Live`; task logs are not in the daemon-wide Logs panel.

- `sqlite3` CLI is not installed in the agentd image; use task APIs or a separate utility container.

- The compose stack starts agentd with `--skip-llm-warmup`, so `/health` does not prove provider connectivity.

- `POST /api/v1/projects/materialize` with nonexistent `source_path` returns an error from the
  seeding step; the project row may already be persisted at that point (verify by ID before
  relying on a clean rollback).

Additional observations:

- **Web nav routes:** `/board` and `/logs` return 404 — this is expected. The sidebar uses
  client-side `<button>` navigation (single-page tab state), not Next.js file routes. Only `/`
  exists as a route.

- **SSE firehose verified live:** `GET /api/v1/events/stream` emitted frames during a task
  retry:

  ``` text
  event: task_retried
  event: token_usage
  event: poison_pill_handoff
  ```

  The stream is silent when no state changes occur — that is expected behavior, not a bug.

- **Task execution reaches terminal states:** tasks end as `QUEUED` (in flight) or
  `FAILED_REQUIRES_HUMAN` after auto-retries. See troubleshooting below for why.

---

### T-027 cycle: P0 journeys (2026-09-29)

Command: `make dev-clean dev-up`, then `make test-e2e` (`-count=1`).

| Run | Stack | Result |
| --- | --- | --- |
| 1-3 | clean (`dev-clean`) | 11/11 pass |
| 4 | reused, one prior run of state | 11/11 pass |
| 5-8 | one stack, no reset (0 -> 26 projects) | 11/11 pass each |

Bugs found and fixed this cycle:

- SQLite pragmas (`busy_timeout`, `foreign_keys`) were applied to one pooled connection only, so other connections dropped writes on `SQLITE_BUSY`, including `TOKEN_USAGE` rows. Now DSN parameters. Zero `SQLITE_BUSY` across the 8-container logs of the 4 accumulating runs; `TOKEN_USAGE` rows durable and monotonic.
- Startup race: agentd dispatched to litellm before it accepted connections, tripping the breaker for 5 minutes and stalling every later journey. `dev-up` now waits for healthy; J11 resets the breaker.
- `make test-e2e` could report `ok (cached)` for a repeat run. Now `-count=1`.
- J14 only checked that a `TOKEN_USAGE` row existed; it now checks the recorded token count and fails on a dropped write.

Not covered: P1/P2 journeys (J05, J06, J12, J13, J15) are not implemented.
