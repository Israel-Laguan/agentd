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

### T-027 cycle: P1 journeys (2026-09-30)

Command: `make dev-clean dev-up`, then `make test-e2e` (`-count=1`) twice on
the same stack.

| Run | Stack | Result |
| --- | --- | --- |
| 1 | clean (`dev-clean`) | 14/14 pass (11 P0 + J05, J13, J15) |
| 2 | same stack, no reset | 14/14 pass |

Journeys implemented this cycle: **J05** (materialization edge cases), **J13**
(OpenAI-compatible intake), **J15** (MCP board export). Deferred with reasons
in `docs/testing/journeys.md`: **J06** (needs a browser; two of its three
expected events do not exist) and **J12** (blocked on T-028 per-request mock
scenarios).

Every P1 spec entry had to be corrected against the running stack before it
could be tested — all three described behaviour the product does not have:

- **J05** said materialize returns 409 for a not-ready workspace. It never
  does: it accepts the plan and returns PENDING tasks, and 409 is what
  `workspace/ready` answers while the workspace is empty. The journey now
  pins that split, plus double-ready as idempotent 200.
- **J13** claimed only "parsed as OpenAI intake". The handler also takes
  `tools` / `tool_choice` / `stream` and emits `tool_calls` only for a tool
  the client declared. `usage` is `omitempty` and never populated, so the
  journey reports it rather than requiring it.
- **J15** named `POST /api/v1/mcp/export` returning all tasks "with outputs".
  There is no such route: the board is a JSON-RPC 2.0 MCP server over
  Streamable HTTP at `POST /mcp`, MCP is off by default (the route is not even
  registered on a stock config), `board.list_tasks` silently caps at 100
  tasks, and no tool exposes task outputs. `docs/mcp-board-export.md` now
  specifies the actual wire format.

Bugs found this cycle:

- **B-004** (fixed): a bad `source_path` returned 500 and left the project and
  task rows persisted — an orphan project whose PENDING tasks could never be
  unlocked, with no route to delete it. `ProjectService.MaterializePlan`
  committed before seeding; it now validates the path first and answers 400.
  Covered by `TestMaterializeRejectsBadSourcePathBeforePersisting` and J05.
- **B-005** (open): `board.list_tasks` without `project_id` silently truncates
  at 100 tasks with no total or cursor. Verified live: a 101-task project
  exported as exactly 100 board-wide, 101 with `project_id`.
- **B-006** (open): `board.list_tasks` ignores `state` when `project_id` is
  also passed. Verified live: `state=FAILED` on a project of COMPLETED tasks
  returned both tasks; the same filter without `project_id` returned none.

Also: `test/e2e/chat-kanban.sh` deleted (superseded by J04 and J07), and
`docs/testing/qa-and-browser-verification.md` / `docs/architecture/repo-layout.md`
repointed. `devenv/agentd/config.yaml` gains `mcp.enabled: true` for J15.

### Housekeeping cycle: close every P1 gap (2026-09-30)

Command: `make dev-clean dev-up`, then `make test-e2e` (`-count=1`) twice back
to back on the same tree. Goal: no P1 leftovers in the PR.

| Run | Stack | Result |
| --- | --- | --- |
| 1 | clean (`dev-clean`) | 17/17 pass (294.5s) |
| 2 | same stack, no reset | 17/17 pass (309.4s) |

Per-journey durations (run 1 / run 2):

| Journey | Run 1 | Run 2 |
| --- | --- | --- |
| J01 boot + warmup | 0.51s | 0.52s |
| J02 board + SSE | 6.51s | 6.53s |
| J03 chat without plan | 0.52s | 0.52s |
| J04 full happy path | 2.66s | 2.64s |
| J05 materialize edges | 2.00s | 1.66s |
| J07 healing handoff | 7.02s | 9.51s |
| J08 unclean restart | 123.13s | 130.80s |
| J09 cascade (faults) | 1.84s | 0.52s |
| J09 breaker (breaker) | 8.56s | 7.05s |
| J10 disk watchdog | 15.56s | 15.52s |
| J11 preference recall | 8.38s | 8.40s |
| J12 tiered escalation | 29.34s | 33.55s |
| J13 OpenAI intake | 0.62s | 0.62s |
| J14 SSE events | 10.32s | 10.32s |
| J15 MCP export | 0.51s | 0.52s |
| J15 large board (B-005) | 0.56s | 0.59s |
| J15 state filter (B-006) | 75.51s | 79.13s |

`dev-up` wall-clock: **484s (8m4s)** — the six-per-service Go image build
(B-007) dominates; see below.

Gates: `go build ./...` clean, `go test ./...` clean, `make check` clean
(loc + minfunc + lint + test + test-mockllm + lint-docs, 43s).

Bugs fixed this cycle:

- **B-002** (P3): the web service rewrote the bind-mounted
  `web/package-lock.json` on every container start. It now runs
  `npm ci --legacy-peer-deps`, which never rewrites the lockfile. The
  accidental lockfile commit (`78781774`) was removed from branch history via
  `git rebase --onto`, so `git diff main -- web/package-lock.json` is empty.
  `make dev-up` leaves `git status` clean.
- **B-005** (P2): `board.list_tasks` silently capped at 100 tasks. The tool
  now takes explicit `limit` (default 200) and `offset`, and both the
  project-scoped and board-wide paths route through the one paginated,
  filter-aware store method. The response stays a bare task array (no breaking
  shape change). Covered by `TestServer_ListTasks_BoardWideExposesAllTasks` and
  `TestJ15_MCPBoardExportLargeBoard`.
- **B-006** (P2): `board.list_tasks` ignored `state` when `project_id` was
  passed. Fixed by the same routing change. Covered by
  `TestServer_ListTasks_StateFilterComposesWithProject` and
  `TestJ15_MCPBoardExportStateFilter`.

Bug filed:

- **B-007** (P3): `make dev-up` builds the same Dockerfile once per agentd
  service — six `CGO_ENABLED=0 go build` invocations per `dev-up`, one per
  service image. The `tiered` → `COMPOSE_PROFILES` change raised the count
  from 5 to 6. This is the dominant `dev-up` wall-clock cost. Candidate fixes
  recorded in the bug; not fixed in this PR.

Journey implemented:

- **J12** (P1, tiered execution): T-028's mock now detects each tiered step
  from its system-prompt suffix and returns the artifact that step commits;
  verify fails by default, so the escalation ladder (mid-fix redos, then a
  strong-model escalate) runs for real. `TestJ12_TieredExecution` materializes
  a complex task on the `tiered` profile, waits for the origin to reach
  COMPLETED via escalation, and asserts the mock's request capture shows both
  a verify-step and an escalate-step request. The `tiered` profile was added
  to `COMPOSE_PROFILES` so `make dev-up` starts it.

Test bug fixed:

- `TestJ15_MCPBoardExportLargeBoard` asserted the board-wide page total equals
  this project's 150 tasks, but the board-wide call spans every project on the
  shared default profile. The assertion now pages the whole board and checks
  every one of the project's tasks appears exactly once, so it is robust to
  board accumulation across runs.

Tooling:

- `checkloc` gained a `devenv/**` → 500-line category (matching the existing
  `docs/**` → 400 pattern and T-028's "server.py under 500" bound); the mock
  server is 406 lines, over the 300 default.

J06 remains deferred (browser tier; two of its three expected task-lifecycle
events do not exist). Not implemented, as instructed.

### T-029 cycle: build the agentd image once (2026-10-01)

Fixed:

- **B-007** (P3): `make dev-up` no longer compiles agentd six times. All six
  agentd services in `devenv/compose.yaml` now take `image: agentd:local`
  instead of declaring their own `build:` block, and a new `make dev-build`
  target (`podman build -t agentd:local .`) runs once per `dev-up` ahead of
  compose. `mockllm` keeps its own build context, so `up --build` is retained
  for it. Verified after `touch internal/queue/recovery/recover.go` and a full
  `make dev-clean`:
  - `grep -c "CGO_ENABLED=0 go build" <dev-up log>` = **1** (was 6).
  - `make dev-up` wall-clock **51s** (was 484s on a tree with edited Go).
  - All six agentd containers report image `localhost/agentd:local`.

`make check` green (exit 0) after the compose, Makefile and docs edits.

`make test-e2e -count=1`: **17/17 in 294.85s** on a clean stack, no regressions
from the compose change; every agentd profile went healthy.

Confirmed live for T-030:

- J08 logged `recovered 2m0s after restart (reset ghost task to READY)` — B-008
  reproduces exactly as documented (PID 1 before and after, so only the
  stale-heartbeat sweep recovers it). J08 is 123.25s of the 294.85s run.

Also in this cycle (T-032 docs hygiene): `journeys.md` stale lines corrected
(status, P1/P2 policy list, B-002 marked fixed); `repo-layout.md` set to
**implemented** with 29 of 34 T-024 checklist boxes verified against the tree and
ticked; `disk.check_interval` decided as **document, don't implement** and
recorded on the `disk.free_threshold_percent` row in `docs/reference.md`.

### T-030 cycle: recover interrupted tasks at boot under PID 1 (2026-10-01)

Fixed:

- **B-008** (P2, major): `BootReconcile` now drops its own PID from the alive
  set before reconciling (`withoutOwnPID` in
  `internal/queue/recovery/recover.go`). `MarkTaskRunning` stamps
  `os.Getpid()`, and in a container agentd is PID 1 before and after a
  restart, so the liveness probe reported the killed daemon's tasks as owned by
  a live process and boot reconcile skipped them. Boot runs before any worker
  starts, so a RUNNING task stamped with our own PID cannot be ours. Only our
  own PID is exempted, so a task owned by a different live daemon sharing the
  home still goes through the probe. No schema change.
- **B-003** (P3): **closed** — see below.

Evidence:

- J08 `recovered **0s** after restart (reset ghost task to READY)`, was
  `2m0s`. The test's wait drops 180s→30s and its health budget 240s→90s.
  J08 itself: **5.55s**, was 123.25s.
- `TestJ08_UncleanKillRecovery` no longer needs the `KNOWN GAP` comment, and
  `journeys.md` is back under its 400-line limit at exactly 400.
- `TestBootReconcile_resetsTaskOwnedByOwnPID` fails on the pre-fix tree
  (`state = RUNNING, want READY`), passes after. Guard test
  `TestBootReconcile_leavesTaskOwnedByOtherLivePID` passes: a task owned by a
  different live PID stays RUNNING with no events.
- `make check` green (exit 0).

**4 consecutive `go test -tags=e2e -count=1 ./test/e2e/...` on one stack
(B-003 re-check): 17/17 each, 195.8s / 195.9s / 185.9s / 195.8s.** Suite
wall-clock down from ~295s.

B-003 outcome: one task was RUNNING immediately after run 4 finished — a
`SLOW_TASK J08` from project `j08-kznz6w` with `os_process_id=1`. Its event log
shows it was **not stuck**: live `LOG_CHUNK tick N` heartbeats and a `RETRY`
("execution timed out: no output within limit") while it re-executed, and it
reached a terminal state on its own ~90s later without intervention, leaving
**0 RUNNING**. So B-003 was the still-executing re-dispatch caught mid-flight,
not a leaked task. J08's own comment already said as much; this run confirms it.

Two setup notes worth keeping:

- The first J08 attempt failed against a **stale image**. `make test-e2e` depends
  on `dev-up`, but the already-running container was created before the source
  edit, and J08's kill/restart reuses that container's filesystem — so the old
  binary kept running and the fix appeared not to work. `make dev-clean` before
  judging a container-level change fixes it. Worth remembering: `dev-up` is not
  enough after editing code that only takes effect on a fresh container.
- `make test-e2e -count=1` cannot be run as written — `make` parses `-count=1`
  as its own option and fails with `invalid option -- 'c'`. The target hardcodes
  `-count=1`, so `make test-e2e` is the same uncached run.
