# Test Execution History (T-029 to T-031)

The T-029, T-030 and T-031 cycles (2026-10-01), moved out of
[results.md](results.md) on 2026-10-03 (S10 follow-up) so the live log had room
under the 400-line docs budget. Earlier cycles are in
[results-history.md](results-history.md). Nothing here is deleted.

## Execution results and notes

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

`make test-e2e`: **17/17 in 294.85s** on a clean stack, no regressions
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
  judging a container-level change fixes it. Worth remembering: a plain
  `podman compose up` is not enough after editing code that only takes effect on
  a fresh container. (`dev-up` now recreates the `agentd_agentd*` containers for
  you, so this only bites when driving compose directly.)
- `make test-e2e -count=1` cannot be run as written — `make` parses `-count=1`
  as its own option and fails with `invalid option -- 'c'`. The target hardcodes
  `-count=1`, so `make test-e2e` is the same uncached run.

### T-031 cycle: never drop a token-ledger write to a busy database (2026-10-01)

Fixed:

- **B-001** (P2): the three token-ledger writes — `AddTokenUsage`,
  `AddUsageDetails` (`internal/kanban/tasks_repo_token_usage.go`) and
  `AppendEvent` (`internal/kanban/events.go`) — were the **only** unwrapped
  writes in the kanban package, despite ~30 neighbouring call sites being
  wrapped, including `AddComment` in the same file as `AppendEvent`. All three
  are now wrapped in `RetryOnBusy`. `Worker.Emit`
  (`internal/queue/worker/worker_support.go`) no longer discards the sink error
  with `_ =`; it logs at Error.

The premise B-001 was filed on turned out to be wrong, and that is the real
finding. `RetryOnBusy`'s ~156 ms of backoff *looks* pointless behind a 5 s
`busy_timeout`. But that 156 ms is only the delay **between** attempts — every
attempt independently gets the driver's own 5 s window, so the wrapper's real
tolerance is **6 × 5 s ≈ 30 s**. Measured with the lock held for three
durations:

| Lock hold | Writes landed |
| --- | --- |
| 8 s | 160/160 (no loss) |
| 25 s | 160/160 (no loss) |
| 32 s | 152/160 (loss returns) |

The cliff sits between 25 s and 32 s, matching 6 × 5 s = 30 s. So `RetryOnBusy`
was always earning its place; it just had no test saying why.

Evidence:

- **Reproduced the loss first.** 8 workers × 20 counter writes while a second
  connection holds the write lock 8 s: `attempts=160 ok=152 failed=8` — a 5%
  drop, with the first failure at 4.7 s. So B-001 was real, not theoretical.
- **Fixed path loses nothing**: the same load through the real
  `store.AddTokenUsage` is `160/160`.
- Retry is safe despite neither write being idempotent (the counter is a
  read-modify-write accumulate; the event mints a fresh uuid per attempt),
  because SQLite guarantees a write that returned `SQLITE_BUSY` did not commit.
  Empirically confirmed: the durable counter equals the successful write count
  exactly in every run — no partial writes, no double-counts.
- `TestRetryOnBusyToleratesLockHeldLongerThanOneTimeout`
  (`internal/kanban/db/busy_window_test.go`) turns the mechanism into an
  always-on ~0.6 s guard: an unwrapped write cannot outlast a single
  `busy_timeout` and fails, the same write through `RetryOnBusy` lands, and
  exactly one row lands so a replayed write would show as a double-count.
- `TestEmitLogsDroppedEvent` failed before the `Emit` fix, passes after.
- `make check` green. Full e2e suite 17/17 in 194.2 s.

A test that was wrong on the first attempt: the initial worker-level test
asserted `Worker` retries a `SQLITE_BUSY` from its `TokenUsageStore`. It passed
vacuously — `RecordTaskTokenUsage` calls both counter writes unconditionally, so
it made two calls whether or not it retried, and the assertion could not fail.
The retry belongs in the kanban store, not `Worker`, so the test was asserting
the wrong layer. Replaced with the store-layer proof. Recorded because a
vacuously-passing test would have shipped as false confidence.

Flake: `TestJ10_DiskWatchdogDedup` failed once at 0.51 s, then passed in
isolation (16.5 s) and on a full re-run. Startup timing, unrelated to the ledger
path.
