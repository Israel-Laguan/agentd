# agentd Test Execution Results

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Historical run outcomes and
> known gaps confirmed live. Append new runs here rather than growing the plan. Cycles
> before T-029 live in [results-history.md](results-history.md).

## Execution results and notes

Cycles before T-029 are in [results-history.md](results-history.md) (moved
2026-10-01, T-033).

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

### SP-012 cycle: provider-outage time to resume (2026-10-01)

Measured in `internal/queue/outage_recovery_test.go`: J09 covered the trip, the
recovery half was unproven, and does not work in every case.

**Time to first probe: 5 min; a failed probe restarts the full timeout.** OPEN holds for
`DefaultBreakerTimeout` (`safety/breaker.go:21`), admits one probe task, closes when it
succeeds. If the probe fails (e.g., during a flapping outage), the breaker reopens and the
5-minute timeout restarts from the failure point, so recovery time extends per failed probe.
It is a package constant, so an operator cannot shorten it.

**Failed tasks split.** The 2 pre-trip failures requeue to READY with `RetryCount`
== 0 (an outage does not consume the retry budget); the trip task is BLOCKED with a
`PROVIDER_EXHAUSTED_HANDOFF`. A long outage ends in a human inbox.

**Defect: the breaker can latch HALF_OPEN forever.** `ProbeLimit` sets `inflight`
= true before dispatch knows it has a task (`loop_dispatch.go:161`), so an empty
queue at the crossing tick spends the probe on nothing; `inflight` is then cleared
only by a result from a task that can no longer be dispatched, so nothing runs
without an operator reset. Reproduced, not committed; sized as T-035.

### T-034 cycle: J06 rewritten, drawer event log covered by component tests (2026-10-01)

J06 wanted `task-started` / `task-claimed` / `task-completed`, and claim and start
write no event row (the same finding J14 made), so it could never pass. Redefined as
**"the drawer renders the task's durable event log"** — the events that exist.

**Fixtures are captured, then adapted.** `GET /api/v1/tasks/{id}/events` was read off
the devenv stack after `TestJ08_UncleanKillRecovery`. `WARNING`, `RECOVERY`,
`TOKEN_USAGE` and `LOG_CHUNK` are the captured rows, with real payloads and nanosecond
timestamps. The `RESULT` row's id, timestamp and payload were trimmed, the drawer
fixture rewrites `task_id` to `task-under-test`, and its `some-other-task` event is
invented. The snapshots pin the wire shape as of 2026-10-01; nothing re-validates them
against the live endpoint, so they do not detect later drift. The stack was reset with
`make dev-clean && make dev-up` first, per the B-008 stale-image trap.

**Three of the item's premises were wrong**, checked against the code:

- "First component tests in `web/`" — `web/` already had 17 vitest files. These are the
  first for these two components, not the first in the repo.
- The drawer's three behaviours were already implemented: the `task_id` filter is
  inline in `task-drawer.tsx`, `eventRefreshKey` is in the effect deps, and the reject
  path clears events. So this was a test-writing job, not a fix — smaller than the
  P1 / ~1 day estimate suggested.
- The component renders events **newest first**, not in API order. The tests pin the
  real order rather than the order the item implied.

**One vacuous test, found and fixed.** The first rejected-fetch test asserted only
that a drawer opened with a failing fetch shows no events — which passes even with the
catch handler deleted, because a fresh drawer's events are already `[]`. Mutation
testing caught it: deleting the handler left the suite green. Rewritten to fetch
successfully once and then fail on refresh, so clearing the stale log is observable; it
now fails against that mutation.

Every other assertion was mutation-checked too. Removing the `task_id` filter, dropping
`eventRefreshKey` from the deps, removing the `.reverse()`, mutating the caller's array,
and removing the empty state each turn the suite red.

`vitest.setup.ts` gained a `scrollIntoView` stub: jsdom does not implement it and
`CommentPanel` calls it on mount, so mounting any component containing it threw instead
of failing an assertion.

`cd web && npm test`: 19 files, 105 tests, green.

**Recorded gap:** no real-browser tier, so CSS layout and click-through are unproven.
That is a product decision for a later sprint, recorded in `journeys.md` rather than
backlogged. The three lifecycle events remain unimplemented — a product change, not a
test gap.

### B-009 cycle: two load-sensitive test waits made robust (2026-10-01)

Both reported flakes are fixed by removing the fixed windows, but **neither
reproduced**, so these are robustness fixes rather than verified root-cause
fixes. Under 2–2.5× CPU oversubscription (20 busy loops on 8 cores) the queue
suite passed 5/5 both with the old 3s budget and the new 30s one, and J10 passed
3/3 at both the old and new windows. Recording that plainly: the change removes a
known timing dependency, it is not a demonstrated cure.

**Breaker scenario.** `waitFor` in `steps_dispatch_test.go` had a hardcoded 3s
deadline shared by every queue scenario, which matches the 3s timeout B-009
recorded exactly. It is now a named `stepWaitBudget` of 30s. Generous is safe
here because every use of `waitFor` polls for an event that either happens on its
own or never happens at all — no scenario legitimately needs to fail fast, since
a failing step returns an error the suite already surfaces.

A larger budget is only safe if still bounded, so `step_wait_budget_test.go` pins
that: `waitFor` must give up, its error must name the budget so a reader can tell
a slow pass from a hang, and `stepWaitBudget` itself must stay positive, above
the 3s that flaked, and at most a minute. The timeout test uses a short explicit
budget via `waitForBudget` so the suite does not pay 30s to prove a timeout works.

**J10.** The dedup window was a single 15s `Sleep` for a crontab that says
`@every 5s`. Under load a pass can start late, so 15s could elapse while fewer
than three extra passes had run and the assertion would prove less than it
claimed. Now a 20s window (3 passes + 5s slack) polled at 500ms, and the test's
context budget goes 90s → 120s to cover it. J10 passes at 20.54s.

Verification: `make check` green; `TestJ10_DiskWatchdogDedup` green under load.

### SP-011 cycle: is re-running a recovered task safe? (2026-10-01)

**Answer: go, with a caveat worth naming.** Nothing cleans the workspace between
attempts, so a re-run is not starting fresh — but the sandbox's determinism and the
worker's output-based commands make it safe in practice. Measured in
`internal/sandbox/rerun_idempotency_spike_test.go`.

**The workspace is not cleaned, confirmed.** A command that writes a file and
sleeps is killed mid-task; the re-dispatch then runs `ls` and sees the file:

```text
FINDING: attempt 2 sees the first attempt's file.
stdout:
marker.txt
```

`BashExecutor` only sets `cmd.Dir` to the workspace and runs. There is no
per-attempt preparation anywhere: `FSWorkspaceManager` has `SecureDelete` (whole
project removal) and `SeedFromPath` (initial seed), and nothing else touches the
directory. So a recovered task resumes on top of whatever the killed attempt left.

**That is genuinely non-idempotent for some commands.** An appending command
reports a different result on the second attempt than the first:

```text
attempt 1 reports "1" lines, attempt 2 reports "2" lines
```

**Why it is still a go.** The non-idempotency lands in the file, not in agentd's
own state. The task's kanban row is reset to READY and dispatched from the start,
so counters in SQLite cannot double-count.

**Open: overlap with a surviving first attempt.** Nothing in recovery verifies or
terminates the killed attempt's process group. Boot reconcile resets a task whose
owner PID is dead immediately, and the stale-heartbeat sweep (2m without a
heartbeat) only resets the kanban row. A command that outlives its daemon can
therefore still be running while the retry starts in the same workspace. The
spike did not measure this; it is the same shape as B-003, closed in S08 for the
live-worker case, not for a command orphaned by a daemon crash.

**A dead retry-awareness hook found on the way.** `models.ExecutionPayload` carries
`PreviousAttempts []string` and `BuildExecutionPayload` populates it from the
event history — but `BuildExecutionPayload` has **no callers**. It is a dead
function, so nothing currently tells a re-run what the previous attempt did. Not a
defect today (no code reads the field), but it is the natural hook for making
re-runs safer, so it is named here rather than left as a surprise.

Follow-up (not yet tracked in `tasks/backlog/`): give the recovery re-run a
per-attempt workspace reset behind a flag, terminate or verify the dead attempt's
process group before re-dispatch, and either wire `PreviousAttempts` into the
prompt or delete it.
