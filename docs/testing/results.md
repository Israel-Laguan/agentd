# agentd Test Execution Results

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Historical run outcomes and
> known gaps confirmed live. Append new runs here rather than growing the plan. Cycles
> before T-029 live in [results-history.md](results-history.md), T-029 to T-031 in
> [results-history-2.md](results-history-2.md) and the pre-S10 cycles of 2026-10-01 in
> [results-history-3.md](results-history-3.md).

## Execution results and notes

This log holds the S10 cycles. Cycles before T-029 are in
[results-history.md](results-history.md) (moved 2026-10-01, T-033); T-029 to T-031 in
[results-history-2.md](results-history-2.md) (moved 2026-10-03); the 2026-10-01 pre-S10
cycles in [results-history-3.md](results-history-3.md) (moved 2026-10-03, B-017).

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
- **Superseded end to end.** Both this fix and B-013 were inert in production until B-016: no
  per-provider breaker could ever open, so neither the probe nor a sibling ever happened.
  **J16 is the first end-to-end evidence for this entry** (see the B-016/J16 cycle below).

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
`cmd/agentd`). `make check` green. **J16 is the first end-to-end evidence for this
entry** — until B-016 was fixed no provider breaker could open at all, so this
behaviour was unreachable in production (see the B-016/J16 cycle below).

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
of 50 — siblings cycle **once per `TaskInterval` (3s in the devenv profiles) with
no backoff**, since a requeue counts as dispatched; the 10s cap applies only to a
tick that dispatches nothing; and the in-package proof that `AdmissionProbeInFlight` needs a reachable trap
— `RecordSuccessFor` goes CLOSED before settling the probe, leaving a CLOSED breaker
that still holds a slot. Seven mutations, all red. Full `make test-e2e` green (16
journeys). The fourth gap, an e2e journey, is blocked: `decideTerminalError`
formats joined provider errors with `%v`, which flattens the quota sentinel, so no
per-provider breaker can ever open — filed as B-016 with a reproduced chain check.
The mock gains the per-model `POST /quota` and `POST /slow_once` that journey will
need (7 new unit tests, no other journey affected).

### B-016 + J16 cycle: the cascade keeps the quota sentinel (2026-10-03)

- **B-016.** `decideTerminalError` formatted the joined provider errors with `%v`, so
  `errors.Is(err, ErrLLMQuotaExceeded)` was false for every cascaded failure and
  `HandleGatewayError` always took the global branch — whose `RecordErrorFor` caller is
  gated on exactly that check, so no per-provider breaker could ever open. Two `%w`
  verbs keep both sentinels and render the same message.
- **Decided, all pinned by tests.** (a) both sentinels survive (all-unreachable stays
  unreachable-only, so an outage never opens a provider breaker); (b) the verdict stays
  keyed on `lookupProvider`, because `admitProvider` takes the probe slot on that key;
  (c) the branches stay mutually exclusive, so one provider's exhausted key cannot stall
  the others, and an unreachable total outage still feeds only the global breaker.
- **Tests, failing first:** 3 new `decideTerminalError` cases with a two-directional
  `errors.Is` check, and 3 new `internal/queue` tests driving the **real**
  `gateway.Router` through `Process` — every earlier test hand-fed the error.
- **J16**, `TestJ16_SiblingsWaitForProviderProbe`, new `provider` profile on :8771 (one
  provider on the mock model `prb-quota`): 3 real 429s open the provider breaker, a
  sibling waits while the admitted probe is in flight (no `PROVIDER_EXHAUSTED_HANDOFF`,
  no HUMAN subtask, re-checked every 500ms across 6s), then all 3 tasks COMPLETE and the
  breaker is CLOSED with no reset. Green alone in 43.8s; full `make test-e2e` green, 18
  tests / 17 journeys, 284s.
- **Spec correction:** the tasks that trip a provider breaker cannot be the ones the
  journey waits on (the quota branch hands them off too), so the trip is done by three
  throwaway "arming" tasks and the asserted-on tasks are created after `open_timeout`.
- **Mutations:** 4 unit + 3 journey, each red — `%v` again (J16 fails at its own arming
  bound, breaker `CLOSED (0 failures)`), the join's `%w` dropped to `%s`, `errors.Join()`
  emptied, the verdict on a hard-coded provider, `gateProvider` always handing off (22.7s),
  `open_timeout` at `5m` (fails on its 90s bound, not hanging). Sources restored
  byte-identical.
- **B-017 filed:** `-race -count=20 ./internal/queue` failed once in each of two runs, in
  two different outage tests, both on the `ErrLLMUnreachable` branch this fix leaves alone.

### B-017 cycle: only the tripping task is escalated (2026-10-03)

The flake was a production race, not test-state leakage. `HandleGatewayError`
asked whether the outage had tripped the breaker with two separate lock
acquisitions:

```go
w.breaker.RecordError(err)
if w.breaker.IsOpen() {          // the breaker's state *now*, not after this record
        w.handoffOrFail(ctx, task, err)
        return
}
w.requeue(ctx, task, fmt.Sprintf("LLM outage: %v", err))
```

Three tasks are dispatched together and all three verdicts are in flight, so the
two that recorded failures 1 and 2 could both be answered with "open" once the
third had been recorded. Measured before the fix: **1.2%** of iterations of
`TestOutageRecoveryFailedTasksSplitBetweenRequeueAndHandoff` alone
(`-race -count=50`, 5 runs: 0/2/1/0/0 failures), and a `ready=1 blocked=2` split
in 5 of 400 probe rounds. `breaker.FailureCount()` was 3 in every failing round —
all three verdicts were accounted for, so nothing was lost in the store.

- **`safety.RecordErrorTrips(err) bool`** records the failure and reports whether
  *that record* left the breaker OPEN, under one lock acquisition. `RecordErrorFor`
  and the plain `RecordError` are now the same helper with the answer discarded.
- **Red first:** `TestHandleGatewayErrorHandsOffOnlyTheTrippingTask` fails 18 of 400
  rounds pre-fix (0.15s, so P(no failure) ≈ 1e-8) and is green after.
- **Ruled out, with evidence:** neither failing test has a channel gate at all
  (`NewDaemon(..., nil, ...)` in both), so the `session task-0 exceeded 1 requests
  in 1m0s` line in the old log window came from a different test in the package;
  and the fake `queueStore` ignores `expectedUpdatedAt` and never returns
  `ErrOptimisticLock`, so no lost store write is possible in them.
- **Mutations, each red:** two calls again instead of `RecordErrorTrips` (15/400
  rounds wrong); `RecordErrorTrips` never reporting a trip (400/400); the threshold
  off by one, `>` for `>=` (400/400, and the deterministic count test red);
  dropping `settleProbeLocked` in the refactor
  (`TestFailedProbeBelowTheTripThresholdStillFreesTheSlot`). Sources restored
  byte-identical.
- **Not fixed here, filed as B-018:** the optimistic-lock write *is* lossy in
  production. Forged against the real store, `UpdateTaskState` with a stale
  `updated_at` returns `ErrOptimisticLock`, and both `requeueClaimedTask`
  (logs it) and `Worker.requeue` (emits an `ERROR` event) drop the write, leaving
  the task QUEUED until `ReconcileOrphanedQueued`'s age threshold elapses. It is
  not what these two tests hit.
- `-race -count=20 ./internal/queue` green 5 runs in a row.
