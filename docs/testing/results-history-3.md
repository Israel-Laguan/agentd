# Test Execution History (pre-S10 cycles)

The SP-012, T-034, B-009, SP-011 and SP-010/SP-013 cycles (2026-10-01), moved
out of [results.md](results.md) on 2026-10-03 (B-017) so the live log had room
under the 400-line docs budget. Earlier cycles are in
[results-history.md](results-history.md), T-029 to T-031 in
[results-history-2.md](results-history-2.md). Nothing here is deleted.

## Execution results and notes

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

If either limit resurfaces, size the two together: both need a per-boot instance ID
(or process start-time check), so that change is costed once. That conditional now
has a ticket home, B-026 (S11, P3), instead of living only in this paragraph.

Both sets of tests are written to fail if the behaviour ever changes, so the
accepted limits cannot silently stop being true — the `withoutOwnPID` mutation
turns SP-010's discriminator test red, and SP-013's claim-count test is
self-checked against a double claim.

### T-033 cycle: docs and tooling leftovers, and the two splits (2026-10-01, reconstructed)

**Reconstructed 2026-10-05 (B-025)** from commit `0dfbf0f5` (PR #115, 8 files, +465/-414)
and the T-033 outcome. It was not written when the cycle ran, which is why the S09
retro counted 5 entries for 6 cycles. Nothing below was re-measured; the figures are
the commit's.

Closed:

- **The five unticked `repo-layout.md` T-024 boxes**, each with a reason rather than a
  tick. Volume reuse was smoke-tested for real: the `agentd_agentd-data` volume
  checksums identically before and after `dev-up`, and all seven profiles report
  healthy. `api-testing.md`'s 7 references were repointed to test names in
  `internal/api/tests/feature/api_http_test.go`, since the old line numbers would now
  point at unrelated code. `folder_audit` was dropped from `.gitignore` (the target and
  script were deleted in T-024). The `podman-runtime-target` memory note was struck:
  there is no memory store in this repo, and the target is already stated in
  `repo-layout.md`, `container-development.md` and the compose header. The final
  sweep's regex was stale, not the tree.
- **The stale-image trap** is in `troubleshooting.md`: J08's kill/restart reuses the
  container filesystem, so `make dev-clean` is needed before `make dev-up` after a
  container-level change, or a fix looks broken. It also notes that podman-compose
  1.3.0's `agentd_litellm_1` name-collision line is noise.
- **Both 400/400 docs were split**, which is the part that widened the scope:
  `results.md` was at the cap after SP-012's entry, so every later entry would have
  failed `checkloc`. `journeys.md` 397 -> 227 (history to `journey-findings.md`, 183);
  `results.md` 400 -> 154 (everything before T-029 to `results-history.md`, 241).
  Nothing was deleted; every heading survives across the new pairs, checked
  programmatically.

`make check` was green on the branch.
