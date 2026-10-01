# Journey Findings

Bugs the E2E journey suite found, and what became of each. Split out of
[journeys.md](journeys.md) on 2026-10-01 (T-033) so the journey specs and the
historical bug record stop competing for the same 400-line docs budget.

Read [journeys.md](journeys.md) for the harness, the per-journey specs and the
pass criteria; read this for why a journey looks the way it does.

## Known Bugs Found by Journeys

### J08: boot reconcile missed tasks owned by a PID-1 daemon (B-008, fixed)

`MarkTaskRunning` stamps `os.Getpid()` (the daemon's own PID) on a RUNNING task, and
`queue.BootReconcile` reset one only when its owning PID was no longer alive. In the
devenv container agentd is PID 1 before and after a restart, so the previous owner
always looked alive and the task was skipped at boot; only the stale-heartbeat sweep
recovered it (observed exactly 2m0s). Fixed in T-030: `BootReconcile` drops its own
PID from the alive set before reconciling. Boot runs before any worker starts, so a
RUNNING task stamped with our own PID cannot be ours — it is an orphan. Only our own
PID is exempted, so a task owned by a different live daemon still goes through the
liveness probe. `TestJ08_UncleanKillRecovery` now allows 30s (health budget 240s→90s)
and logs time-to-recovery. Accepted limit: two daemons in separate PID namespaces both
at PID 1 sharing a home would collide; that needs a per-boot instance ID.

### J10 (fixed in the devenv fixture, not product code): the disk watchdog's cadence was unreachable from config

`devenv/agentd/config.disk.yaml` set `disk.check_interval: 5s`, but no Go
config struct reads that key — `DiskConfig` (`internal/config/runtime_controls.go`)
carries only `FreeThresholdPercent`, and the watchdog's interval comes from the
`disk-watchdog` line in `<AGENTD_HOME>/agentd.crontab`
(`internal/config/cron.go`'s `applyCronJob`). The key was silently ignored, so
the watchdog stayed on the default `*/10 * * * *` and J10 could not observe a
pass (let alone a deduped second one) inside an e2e run. Fixed by adding
`devenv/agentd/crontab.disk` (an `@every 5s disk-watchdog` entry) and
bind-mounting it into the `agentd-disk` service, and by deleting the dead key.

This is a fixture bug, not a product bug — but **`disk.check_interval` reads
like a real knob and isn't one**. Decided in T-032: **document, don't implement.**
The crontab is the only input, and a Go-side interval would mean a second
scheduler for one job; `docs/reference.md` now says so on the
`disk.free_threshold_percent` row. No product change made.

### J14: task completions were never published to the event bus (product bug, fixed)

`RESULT` — the event that reports a task's outcome, and the one the web UI's
live board needs to move a card to done — was appended inside the store's DB
transaction (`Store.UpdateTaskResult` → `kanban/db.AppendTaskResultEvent` via
`FinishTaskResultSideEffects`, in the same transaction that flips the task to
COMPLETED). Durable, yes, but it never passed through `bus.EventEmitter.Emit`,
so it was never published to the bus.

SSE subscribers therefore saw a task begin producing output and then go
silent; completion only became visible on a later poll or reconnect. Verified
directly: a task's durable log held a `RESULT` row while the stream carried
only `[WARNING TOKEN_USAGE LOG_CHUNK]`.

Fixed by adding `models.EventBroadcaster` (publish-without-persisting,
implemented by factoring the fan-out out of `Emit`) and broadcasting the
RESULT from the worker's commit path. It must not be re-persisted — that
would double the event-log rows.

Also corrected here: the spec's `task-started` / `task-claimed` /
`task-completed` events do not exist. `ClaimNextReadyTasks` and the
READY → RUNNING transition write no event row, so there is nothing to stream
for them. J14 asserts the events that do exist.

### SQLite drops events under contention (product bug, fixed)

Running the full suite repeatedly against one long-lived stack made J04/J14
fail intermittently — a task completed but its `LOG_CHUNK` / `TOKEN_USAGE`
events were missing from both the stream *and* the durable log. The daemon logs
showed why:

```text
ERROR failed to persist token usage ... err="add token usage: database is locked (5) (SQLITE_BUSY)"
WARN  memory touch failed ... err="touch memories: database is locked (5) (SQLITE_BUSY)"
```

**Root cause.** `internal/kanban/db/open.go` applied `busy_timeout` and
`foreign_keys` with `db.ExecContext` on the connection pool. Both are
*per-connection* settings, so only the one connection that happened to run
them got them; every other pooled connection had a 0ms busy timeout and failed
instantly under write contention. The retry wrapper (`RetryOnBusy`, ~150ms
total) could not compensate. A completed task could therefore be missing the
rows describing how it got there, and a dropped `TOKEN_USAGE` row means the
token ledger under-reports spend.

**Fix.** The pragmas are now passed as `_pragma=` DSN parameters, so the driver
applies them to every connection (`connectionDSN`, covered by
`TestOpenAppliesBusyTimeoutToEveryConnection`, which fails without the fix).
Foreign-key enforcement, previously silently off on most connections, now
applies everywhere.

Contention comes from two suite artefacts:

- **Task accumulation and state drift.** J08 SIGKILLs the daemon mid-task; its
  recovery marks the killed task complete but leaves a sibling RUNNING for 60s.
  Each run accumulates more projects, tasks, and a small number of long-lived
  RUNNING/FAILED states. Across 4 runs: projects grow 0→26, tasks 0→29.
- **No board cleanup.** Accumulated projects are never deleted (no DELETE route),
  so the dispatch loop and status summarizer scan more rows with each run.

Verified: 4 consecutive test runs on one stack (0→26 projects, zero SQLITE_BUSY,
TOKEN_USAGE rows durable and monotonic) confirm the pragma fix eliminates the
write drops under real contention. J04/J14 remain stable across runs.

For clean-database runs, use `make dev-clean dev-up` (drops volumes); plain
`make dev-down` keeps the named volumes and accumulates state for stress
testing. Retrying or queueing on a still-busy DB remains a product decision
and is not implemented here.

### Suite hygiene: breaker poisoning, test cache, readiness

- **Startup race.** `up -d` returns once containers exist, and podman-compose
  does not reliably honour `depends_on: service_healthy`, so early tasks could
  reach litellm while it still refused connections. Three failures trip the
  circuit breaker (`defaultBreakerFailures`) and block dispatch for
  `defaultBreakerTimeout` (5m), leaving every later journey stuck in READY.
  `make dev-up` now waits for all containers to report healthy, and J11 resets
  the breaker like J07/J09 do.
- **Go test cache.** `make test-e2e` passes `-count=1`; without it a repeat run
  reports `ok (cached)` in 0s, which would falsely satisfy "2 clean runs".

### J11: preferences could not reach a worker prompt (product gap, fixed)

Four independent breaks, each sufficient on its own to make the journey's
goal unreachable — the last one only surfaced once the others were fixed, on
the second run against a stack that already held preferences from a previous
run. Verified on the live stack: `POST /api/v1/preferences`
correctly writes a `USER_PREFERENCE` memory row, and recall *with* a
`user_id` returns it — but:

1. The worker's execution-time recall passed `userID=""`
   (`internal/queue/worker/worker_messages.go`), so `RecallMemories` never
   added its `USER_PREFERENCE` branch to the query. A blank user is an
   exclusion, not a wildcard.
2. `memoryFormatLessons` deliberately skips `USER_PREFERENCE` rows, and
   `memory.FormatPreferences` was never called from the worker — so even
   recalled preferences were dropped before the prompt.
3. Nothing carried a user identity to execution: `models.Task` had no
   `user_id`, and phase continuations, breakdowns and handoff children are
   all created long after the chat turn.
4. Once the first three were fixed, a user with more preferences than
   `RecallTopK` (default 5) still lost the newest ones: as a branch of the
   bm25-ranked FTS query they competed with lessons for the same LIMIT, so
   saving a preference could silently stop having any effect. Fixed by
   recalling preferences in their own newest-first query, under their own
   ceiling, with no requirement that they match the task's intent terms.

Fixed by stamping the requesting user on the **project** (new
`projects.user_id`, schema v19) rather than per task, so tasks created later
inherit it for free; reading `X-Agentd-User` on materialize (the header chat
already uses, and preferred over a body-supplied value); and appending
`memory.FormatPreferences(recalled)` as its own system message after the
stable prefix, so anonymous prompts are byte-identical to before.

There was also no way to *observe* the prompt: the devenv mock logged only
correlation metadata, which is how the old memory-recall demo script (deleted
in T-025) ended up simulating its own success in Python. The mock now appends
every request body to a JSONL log, served back at `GET /requests`.

### J09 (fixed in the devenv fixture): the faults profile could never trip the breaker

The journey spec assumed one `faults` profile could cover both cascade and
breaker. It can't: cascade requires a *live* secondary, and tripping the
breaker requires *every* provider dead. With a working secondary, a worker
request always succeeds via cascade, so no failure is ever recorded. Split into
two profiles (`faults` for the cascade, new `breaker` for the trip) rather
than weakening either assertion.

Two further spec corrections, both verified against the running stack:

- **The trip threshold is 3, not 5.** `internal/queue/safety/defaultBreakerFailures`
  is 3, and it is a constant with no config override. The test asserts the
  observable contract (breaker OPEN + HUMAN child) rather than a count, so
  retuning the threshold won't break it.
- **`/v1/chat/completions` returns HTTP 200 even when every provider is dead**,
  answering with a `"[SYSTEM] Communication with AI core timed out"` message.
  So chat is not a usable failure signal for the breaker half. Confirmed
  directly against the all-dead `breaker` profile on :8770.

---
