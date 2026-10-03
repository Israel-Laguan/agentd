# agentd Test Execution Results

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Historical run outcomes and
> known gaps confirmed live. Append new runs here rather than growing the plan. Cycles
> before T-029 live in [results-history.md](results-history.md) and T-029 to T-031 in
> [results-history-2.md](results-history-2.md).

## Execution results and notes

Cycles before T-029 are in [results-history.md](results-history.md) (moved
2026-10-01, T-033); T-029 to T-031 are in [results-history-2.md](results-history-2.md)
(moved 2026-10-03).

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

### SP-010 and SP-013 cycle: two P3 limits, measured and accepted (2026-10-01)

Both were half-done spikes whose remaining question was a bounded, accepted risk.
Both are now measured rather than assumed, and both are recommended **dropped with
the limit recorded** — the fixes are schema changes with a poor cost/benefit ratio
against a 2-minute worst case that only follows a host reboot.

**SP-013 — no single-instance guard.** Confirmed by reading the start path: a second
`Open` on an already-initialised home succeeds and writes to it. There is no lock
on the home; the only `flock` in the tree is on a per-document cache in
`filecontext`, unrelated to the daemon. With two stores on one database
(`internal/kanban/two_daemons_spike_test.go`):

- **No double-dispatch.** `ClaimNextReadyTasks` runs `BEGIN IMMEDIATE`, so two
  daemons racing the same READY task hand it out exactly once — A claimed 1, B 0.
- **No false recovery.** Daemon A's boot reconcile leaves daemon B's task RUNNING,
  because B's owner PID is genuinely alive.

So the two feared failure modes do not occur. The remaining risk is both daemons
polling and competing on the same queue, which costs throughput rather than
correctness. **Dropped**, with the limit recorded.

**SP-010 — PID reuse after reboot.** Confirmed: a RUNNING task whose owner PID is
held by an unrelated live process is not recovered at boot, and waits for the
stale-heartbeat sweep (~2m). Root cause, stated as an assertion: given only a PID,
PID reuse and a live second daemon are *indistinguishable* — both leave the task
RUNNING. Fixing it needs a per-boot instance ID or process start-time check, which
is a schema change after v19. **Dropped**, with the limit recorded.

Note T-030's fix does not touch this: it resets tasks owned by the daemon's *own*
PID, and a reused PID is by definition not our own.

Both sets of tests are written to fail if the behaviour ever changes, so the
accepted limits cannot silently stop being true — the `withoutOwnPID` mutation
turns SP-010's discriminator test red, and SP-013's claim-count test is
self-checked against a double claim.

### S10 cycle: breaker recovery and re-run result (2026-10-03)

Fixed:

- **T-035a (latch).** A HALF_OPEN probe slot taken on a tick that dispatched nothing is
  now released (`CircuitBreaker.ReleaseProbe`, called from `dispatch`). The failing test
  (`TestDispatchReleasesProbeSlotWhenQueueIsEmptyAtTimeout`) was red first, then green;
  neutralising the release turns it and the breaker unit test red.
- **T-035b.** `breaker.open_timeout` (default `5m`, non-positive falls back to it) is read
  from config. J09 now covers the recovery: the breaker profile has a third provider on a
  mock model the journey takes down and back (`POST /outage` on the mock LLM) and a 10s
  timeout. Measured: breaker CLOSED and a waiting task COMPLETED **19s** after the provider
  returned, with no reset call. With `open_timeout` put back at `5m` the journey fails at
  its 55s bound.
- **T-036a.** J08 now asserts the re-run's result. Measured: COMPLETED with the final line
  in RESULT; a wrong marker fails it. J08 is **34.6s** (was 123s) because the slow command
  is 30s, not 60s.
- **T-036c.** Deleted `BuildExecutionPayload`, `ExecutionPayload.PreviousAttempts` and
  their test; J08 shows a re-run completes without knowing the earlier attempt. The
  contract is in `docs/architecture/recovery-rerun.md`.

Found:

- J08's 60s fixture could never complete: the re-run was killed at the 60s inactivity
  limit on every attempt and evicted with `POISON_PILL_HANDOFF`. Cause is B-011, a
  product defect (the inactivity timer is per stream, so a command silent on stderr is
  killed at the limit however much it prints on stdout). Filed for S11.
- Per-provider breakers are never probed, so an open one stays open until reset (B-012,
  from reading the code, not yet reproduced). Filed for S11.
- T-036b is not buildable as written: the workspace is per project and a project's seed
  is not recorded. Split to B-010 with a recommended shape.

`make check` green. J09 `TestJ09_BreakerOpens` and J08 `TestJ08_UncleanKillRecovery`
pass against the rebuilt stack; the rest of `make test-e2e` was not run.

### S10 follow-up: B-011, shared sandbox inactivity timer (2026-10-03)

Fixed:

- **B-011.** `BashExecutor` gave stdout and stderr each their own inactivity timer, so a
  command that printed only to stdout was killed at `sandbox.inactivity_timeout`. Both
  streams now share one `activityTracker` (`internal/sandbox/inactivity.go`): a read on
  either resets it, and it stops once both have ended. Failing test first
  (`TestBashExecutorInactivityIsSharedAcrossStreams`, 500ms limit): chatty stdout and
  chatty stderr each timed out before the fix and complete now; a silent command and a
  command that closes stdout then goes silent are still killed.
- Mutation checks: a separate tracker per stream turns the two chatty cases red; stopping
  the timer on the first EOF turns the closed-stdout case red (it survived until that
  case was added).
- J08's `SLOW_TICKS` stays at 30 (restoring 60 is optional); its comment in
  `devenv/mockllm/server.py` no longer cites B-011 as a live limit.

### S10 follow-up: B-012, per-provider breakers probe after the timeout (2026-10-03)

Reproduced, then fixed:

- **B-012.** Reproduced with `TestWorkerProviderBreakerProbesAfterTimeout`: a tripped
  provider breaker, clock moved past the timeout, and the worker still made 0 gateway calls.
  Both worker gates (`worker.go`, `worker_batch.go`) tested `IsOpen()`, and nothing ever
  moved a provider breaker out of OPEN or recorded a success on it.
- The gates now call `CircuitBreaker.Admit()`: after the timeout one task takes the
  HALF_OPEN probe slot, and siblings are handed off exactly as while OPEN. A successful task
  (`commitSucceeded`) closes the provider's breaker; a quota error reopens it and restarts
  the timeout; a probe that ends with no verdict releases the slot (same latch as T-035a).
- **Decision:** provider breakers honour `breaker.open_timeout` (`NewProviderBreakersWithTimeout`,
  wired in `cmd/agentd/wiring.go`). One knob, and a second hard-coded `5m` would be a
  surprise. Provider breakers have their own probe-slot release because the worker, not the
  dispatch tick, takes their slot.
- Mutation checks, each red for the stated reason: gate back to `IsOpen` (5 tests); drop the
  deferred release (release test); drop `recordProviderSuccess` in `commitSucceeded`
  (probe test); ignore the configured timeout (timeout test); `Admit` never reporting a
  probe (sibling, release and timeout tests); batch gate back to `IsOpen` (batch test). A
  success hook I first added in `handleLoopResult` survived its mutation because the loop
  already commits through `commitSucceeded`, so it was deleted.
- Not covered by a test: the one-line wiring of `cfg.Breaker.OpenTimeout` into the registry.

### S10 follow-up: B-010 / T-036b, opt-in workspace reset on recovery (2026-10-03)

Built:

- **Baseline decision:** a persisted "started empty" bit (`projects.started_empty`, schema
  v20), not a recorded `source_path`. Evidence: `DraftPlan.StartEmptyWorkspace` is already
  known at materialize time and only needed persisting; "empty" is the one starting state
  that can be restored exactly, whereas a recorded `source_path` restores what the directory
  holds *now* and still cannot cover hand-populated projects. SP-010 rejected a schema change
  because the cure was worth ~2m; this one is a single additive column on the v19 template,
  existing rows default to 0 (the reset refuses them, the safe direction).
- **`recovery.clean_workspace_on_recover`** (default off), boot reconcile only. It refuses and
  fails the task as FAILED_REQUIRES_HUMAN (`RECOVERY_RESET_REFUSED`) when the project did not
  start empty, has another COMPLETED or RUNNING task, or the reset errors; one reset per
  project; `FSWorkspaceManager.ResetProjectDir` is jailed and refuses the root.
- Failing tests first (recovery package, red by compile then by assertion). Mutation checks,
  each red: skip the started-empty check; drop either half of the COMPLETED/RUNNING guard;
  reset per task instead of per project; do not fail the task on refusal; ignore the reset
  error; do not call the reset from boot; reset without the jail, without the root refusal,
  or by deleting the directory itself; daemon not passing the option; store not persisting
  the bit, or persisting it for seeded projects.
- **J08** now runs with the flag on (`devenv/agentd/config.yaml`). The mock's slow command
  drops `attempt.marker` and prints `carried-over` if it is already there; J08 waits for the
  first attempt's output before the kill, then asserts the re-run's RESULT lacks
  `carried-over` and a `RECOVERY_WORKSPACE_RESET` event exists. With the flag off J08 fails
  with the marker carried over (measured). J08 passes in 42s.
- Removed the mock test that pinned `SLOW_TICKS` under the inactivity limit (obsolete since B-011).

Verified: `make check` green after the stack was rebuilt (`dev-clean`, `dev-up`); full
`make test-e2e` passes (J01 to J15, 246s), the first full run since S10.

### B-013 cycle: siblings wait for a provider probe (2026-10-03)

While a provider breaker's probe is in flight, sibling tasks for that provider now go
back to READY (no gateway call, no handoff, no RETRY event) instead of
`PROVIDER_EXHAUSTED_HANDOFF`; an OPEN breaker still hands off. `Admit` reports the
refusal reason atomically. The first sibling test failed with the task BLOCKED, then
passed. Mutation checks, each red: `Admit` never reports "in flight", helper always
hands off, requeue with a payload, probe slot not released, OPEN also waits, and
`cfg.Breaker.OpenTimeout` dropped from the provider registry (new test in
`cmd/agentd`). `make check` green; no devenv journey run (no profile changed).

### B-014 cycle: only the probe holder settles the probe (2026-10-03)

The HALF_OPEN probe slot now has an owner: `AdmitFor`/`RecordErrorFor`/
`RecordSuccessFor`/`ReleaseProbeFor` name the task holding it, and a verdict from
any other task moves the failure count and the breaker state without settling the
slot. The dispatch path's unowned slot and the plain unscoped `Record*`/
`ReleaseProbe` are unchanged. 13 mutations; 9 red, 4 equivalent (`RecordSuccess`
clearing unconditionally, and the three call sites left unscoped) — all four are
inert because a closed breaker takes no slot and the `OPEN` → `HALF_OPEN`
transition resets the slot, both pinned by tests. `make check` green; no devenv
journey run (no profile changed).

### B-013 gaps: real store, dispatch soak, admission invariant (2026-10-03)

Closed three of four open B-013 test gaps, tests only. Real-store requeue in
`internal/kanban` (depguard's `queue-test-isolation` and `kanban-test-isolation`
rules keep the real board and the breaker package apart, so the store half lives
there); dispatch-loop soak at `TaskInterval` 20ms measuring 49 ticks against a bound
of 50; and the in-package proof that `AdmissionProbeInFlight` needs a reachable trap
— `RecordSuccessFor` goes CLOSED before settling the probe, leaving a CLOSED breaker
that still holds a slot. Seven mutations, all red. Full `make test-e2e` green (16
journeys). The fourth gap, an e2e journey, is blocked: `decideTerminalError`
formats joined provider errors with `%v`, which flattens the quota sentinel, so no
per-provider breaker can ever open — filed as B-016 with a reproduced chain check.
The mock gains the per-model `POST /quota` and `POST /slow_once` that journey will
need (7 new unit tests, no other journey affected).
