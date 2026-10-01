# Repo Layout: Folders by Concern

Status: **implemented** (T-024 moves landed; the checklist below was verified
against the tree on 2026-10-01 — 29 of 34 boxes ticked, 5 still open and listed
as unticked). The paths marked "target" now exist. The work was split into T-024 (the moves)
and sprint S07 (US-007): an end-to-end journey suite in `test/e2e/` that finishes
what the demo scripts started, after which T-025 deleted `scripts/demo/`
(done 2026-09-30 — see "Demo scripts").

## The rule

Every folder at the root exists for one reason: the concern it serves. A folder
that doesn't fit one of these is suspect.

| Concern | Question it answers | Folders |
| --- | --- | --- |
| Compile time | Does this end up in what we ship? | `cmd/`, `internal/` (Go binary), `web/` (UI), root `Dockerfile` |
| Test time | Does the product behave correctly? | `*_test.go` next to the code they test (Go requires the same directory), feature tests such as `internal/api/tests/feature/`, and `test/` for checks that need a built binary, image or running stack |
| Dev time | Is this change OK to push or merge? Is my environment OK to work in? | `tools/`: merge gates, CI helpers and hand-run diagnostics, grouped by purpose, not language |
| Local run | How do I run the product while developing it? | `devenv/` |

Documentation and planning (`docs/`, `tasks/`) sit beside these. Root files are
limited to those a tool expects at the root: `go.mod`, `Makefile`, `Dockerfile`,
`.dockerignore`, lint configs, top-level guides.

### Why not a `src/` folder

In Rust and JS a `src/` folder is how tooling finds the code. In Go the same
signal is `cmd/` + `internal/`, and the compiler enforces it: nothing outside
those two reaches the binary. A `src/` would cost one of two things:

- **`go.mod` stays at the root:** every import gains `/src`. That's 1,408
  import lines across 599 files, changed for a word that says nothing.
- **`go.mod` moves into `src/`:** the module root stops being the repo root.
  Makefile, CI and Dockerfile all need `-C src`, and repo tools can no longer
  import `internal/`.

`web/` already follows its ecosystem's convention on its own (`app/`, `lib/`).

### Naming rules for new files

| Put a file in… | when… | never |
| --- | --- | --- |
| `internal/<pkg>/` | it is product code, or a Go test of that package | repo tooling |
| `test/container/` | it runs the test suite inside an image | app runtime config |
| `test/e2e/` | it passes or fails against a running agentd | anything `go test` could run in-process |
| `tools/<name>/` | it is a Go program that inspects the repo, run as `go run ./tools/<name>` | code product packages import |
| `tools/ci/` | it is shell that CI runs as a merge gate | local convenience scripts |
| `tools/diag/` | it is a utility a developer runs by hand to check an endpoint or environment (e.g. "is this LLM endpoint reachable and conformant?") | anything CI gates on, or anything that asserts agentd behaviour |
| `devenv/<service>/` | a compose service needs it to run locally | tests, CI |

"e2e" means a user journey checked against the running `devenv/` stack
(`test/e2e/`, run by `make test-e2e`). In-process `httptest` suites are
feature tests.

There are no demo scripts. Behavior worth proving is a Go test, an e2e journey
or a merge gate. Otherwise it gets deleted.

## Target layout

```text
cmd/  internal/  web/        compile time (unchanged)
internal/api/tests/feature/
  api_http_test.go           was e2e/http_test.go
test/                        test time, outside the source tree
  container/Dockerfile       was Dockerfile.test (+ Dockerfile.dockerignore)
  e2e/journeys_*_test.go     the e2e journey suite (J01-J15); was scripts/chat-kanban-qa.sh, deleted in T-027
tools/                       dev time
  checkloc/                  was tools/checkloc/
  checkminfunc/              was tools/checkminfunc/ (+ baseline)
  ci/verify_gomod.sh         was tools/ci/verify_gomod.sh
  ci/report_failure.sh       was tools/ci/report_failure.sh
  diag/llm-smoke.sh          was tools/diag/llm-smoke.sh
devenv/                      local run (rootless Podman)
  compose.yaml               was devenv/compose.yaml; pins `name: agentd`
  agentd/config.yaml         was devenv/agentd/
  litellm/                   was devenv/litellm/
  mockllm/                   was devenv/mockllm/; the only mock LLM
```

`scripts/`, `dev/` and `e2e/` disappear. Deleted outright: `Dockerfile.init-test`,
`scripts/verify-init.sh`, `scripts/run-tests.sh`, `scripts/integration-test.sh`,
`scripts/test-env.sh`, `scripts/mock_llm.py`, the tracked
`scripts/checkminfunc/baseline.bak` and the `make folder-audit` target.
`scripts/demo/` stayed until the S07 journeys covered it; T-025 deleted it
2026-09-30 once every gated journey passed (see "Demo scripts").

## Decisions

### `devenv/` for the local stack

| Convention | Name | Fit |
| --- | --- | --- |
| golang-standards `deployments/` | config for real deployments | Wrong: this is a mock LLM + dev web server |
| Grafana `devenv/` | local dev environments | Matches |
| current `dev/` | — | Ambiguous; collides with `/dev/null` in every grep (the SP-007 audit grep for `dev/` returned more `/dev/null` hits than real ones) |

The compose file moves in with it. Verified against podman-compose 1.3.0
(`podman_compose.py`): relative paths resolve from the compose file's
directory, and the project name falls back to the directory basename
(`devenv`) unless a top-level `name:` is set. Pinning `name: agentd` keeps the
existing `agentd_agentd-data` volume and the `agentd_<service>_<n>` container
names the docs use. `make dev-up`, `make dev-down` and `make dev-logs` replace
typing the path.

### `tools/` for merge gates and diagnostics

`checkloc` and `checkminfunc` are Go programs with tests, so `scripts/` was
the wrong name. `internal/tools/` would put repo linters in the product
package tree. `tools/` is the golang-standards name, and Go 1.24's `tool`
directive retired the old `tools.go` meaning. The CI shell helpers join them
under `tools/ci/` because they serve the same concern. `llm-smoke.sh` goes to
`tools/diag/`: it checks whether a given OpenAI-compatible endpoint is
reachable and supports text, JSON mode and tool calling. It never touches
agentd, so it isn't an e2e test, and CI doesn't run it, so it isn't a gate.

### `e2e/` folds into `internal/api/tests/feature/`

`e2e/http_test.go` calls `api.NewHandler` with an in-memory store through
`httptest`, the same shape as the feature tests, and `make test` already runs
it. There are no test or type name collisions with package `api_test`. Its
`request`, `assertStatus`, `assertJSONField` and `decodeBody` helpers are
identical to `server_test.go`'s, so drop them. The focused run is
`make test PKG=./internal/api/...`. The `make test-e2e` name is freed and
T-026 reuses it for the real journey suite.

### One mock LLM

Keep `devenv/mockllm/server.py`, the mock the compose stack uses and the only
one that follows current agentd flows (plan, intent, scope, task correlation).
`scripts/mock_llm.py` was only printed by `test-env.sh`, so it went.
`scripts/demo/mock-provider.py` went with the demo scripts (deleted in T-025).

### Unreferenced files: deleted

Owner confirmed 2026-09-27. Nothing in the Makefile, CI or docs referenced them.

| File | Why it can go |
| --- | --- |
| `Dockerfile.init-test` + `scripts/verify-init.sh` | `cmd/agentd/init_test.go` covers home, `global.db`, WAL and crontab. The one gap, the `projects/`, `uploads/` and `archives/` dirs, becomes three assertions in `TestInitCreatesHomeDatabaseAndWAL` |
| `scripts/run-tests.sh` → `scripts/integration-test.sh` | Replaced by the feature tests plus `test/e2e/`. It also runs `pkill -9 -f "agentd.*start"`, `rm -rf .agentd` in the repo, and writes reports into `docs/` |
| `scripts/test-env.sh` | `make dev-up`, or `cd web && npm run dev` for mock mode. Its no-container steps move into [container-development.md](../container-development.md) |

## Demo scripts

The scripts were meant to prove end-to-end behaviour and never did it well.
Sprint S07 finished the job: each beat became an e2e journey (J07–J12 in
[journeys.md](../testing/journeys.md)), and T-025 deleted a script once its
journey passed or was explicitly deferred. The audit below checked each
script's real pass/fail logic (not its comments) against the existing Go
tests. It fed the journey gotchas and lists the Go tests T-025 also added.
Nothing in the Makefile, CI or Go tests depended on `scripts/demo/`, and the
directory was deleted 2026-09-30.

| Script | What it actually asserts | Already covered by | Gap | Go-test follow-up (T-025) |
| --- | --- | --- | --- | --- |
| `restart-mid-task.sh` | daemon restarts after SIGKILL and `system/status` returns 200. Task state is printed, never checked | `internal/queue/restart_mid_task_test.go` (RUNNING → READY, recovery task, events; fake store), `internal/kanban/task_heartbeat_test.go` `TestReconcileGhostTasksResetsDeadPID` (SQLite) | reopening a file-backed `global.db` on the same home after an unclean close, then `BootReconcile` | **Convert**: test in `cmd/agentd/` with a `t.TempDir()` home. Seed a RUNNING task with a dead PID, drop the handle without `Close`, reopen via `openRuntime`, `BootReconcile`, assert not RUNNING and status 200 via `api.NewHandler` |
| `provider-fallback.sh` + `mock-provider.py` | cascade: 2xx with "fallback ok"; breaker: 5 × timeout message, then breaker OPEN | `internal/gateway/beat2_provider_fallback_test.go`, `router_unreachable_test.go` (fake providers); `internal/queue/safety/breaker_semaphore_test.go` | the real openai adapter built by `NewRouterFromConfigs` with a dead primary and a live secondary | **Convert the cascade**: test in `internal/gateway/` with primary `http://127.0.0.1:1` and an `httptest.Server` secondary; assert `ProviderUsed == "secondary"` and the content. **Drop the breaker probe**: only the queue worker calls `RecordError` (`internal/queue/worker/worker_handoffs.go`), so chat traffic can never open the breaker and the probe can't pass as written |
| `disk-watchdog.sh` | exactly one HUMAN "Disk space critical" task in `_system`, event in SSE or log, dedup on re-poll | `internal/queue/disk_watchdog_test.go` (task, event, dedup, silence above threshold), `internal/kanban/system_project_test.go` (dedup on SQLite) | only config → daemon wiring. The watchdog doesn't run at boot (cron `*/10`), so the probe waits up to 10 minutes | **Delete** |
| `memory-recall.sh` | `POST /api/v1/preferences` returns `saved` and runtime heap counters are non-zero. The "retrieval" check builds the expected string from its own seed file, so it can't fail | `internal/api/controllers/preferences_test.go`, `internal/memory/recall_test.go` (fake store) | the SQLite `USER_PREFERENCE` recall path (FTS + `user_id:` tag match, `internal/kanban/memories_repo.go`) | **Delete**, and add the test the doc claims exists: in `internal/kanban/memories_repo_test.go`, record a preference as the API does, recall it by user and intent, check another user doesn't get it, check `FormatPreferences` output |
| `tiered-harness.sh` | only facts about the fixture files (baseline passed, costs non-zero). It never compares tiered with baseline and runs no Go code. Its pricing lives only in bash, with tier names that don't match config | nothing needed | none. A "tiered < baseline" test over hand-written fixtures would test the fixtures | **Delete** with `fixtures/` (the `fixtures/tiered/` set is already unused). Mark the cost table in [tiered-execution.md](../tiered-execution.md) as illustrative |

`lib/demo-common.sh` was only sourced by these scripts and was deleted with
them. [harness-reliability.md](../harness-reliability.md) and [demo.md](../demo.md)
now point at the journeys that prove them, and the Beat 2.4 claims
(`total_memories > 0`, `preferences_count > 0`) were removed because nothing
produced them.

## Side findings

- The `__pycache__` files the audit listed are not tracked. Only
  `scripts/checkminfunc/baseline.bak` is tracked despite `.gitignore`.
- `make folder-audit` points at a tool that was removed with its audit doc
  (see [folder-grouping.md](folder-grouping.md)).
- `.dockerignore` patterns are anchored at the context root. `*_test.go`,
  `*.test.go` and `*.md` only match root-level files, so they are close to
  no-ops. The trap: "fixing" them to `**/*_test.go` would silently strip the
  tests from the test image, because Podman reads the root `.dockerignore` for
  `podman build -f test/container/Dockerfile .`. The test image gets its own
  `test/container/Dockerfile.dockerignore` (verified: Podman 5.4.2 prefers a
  `<containerfile>.dockerignore` sibling). `.e2e/` matches nothing.

## Migration checklist (T-024)

One PR, in this order, with `make check` and `make podman-test` green at the
end. Rootless Podman + podman-compose 1.3.0 stays the target (see "Container
deploy notes" in [litellm-integration.md](../litellm-integration.md)).

### 1. Deletions

- [x] `git rm` `Dockerfile.init-test (deleted)`, `scripts/verify-init.sh`, `scripts/run-tests.sh (deleted)`, `scripts/integration-test.sh (deleted)`, `scripts/test-env.sh (deleted)`, `scripts/mock_llm.py (deleted)`.
- [x] `git rm --cached tools/checkminfunc/baseline.bak` (before the `tools/` move).
- [x] Add the three directory assertions to `TestInitCreatesHomeDatabaseAndWAL`.

### 2. `devenv/`

- [x] `git mv dev devenv`; `git mv docker-compose.dev.yml devenv/compose.yaml`.
- [x] In `compose.yaml`: add `name: agentd`, and set `./devenv/…` → `./…`, agentd `context: .` → `..`, `./web` → `../web`. Update the header usage comments.
- [x] Update the header comments in `devenv/agentd/config.yaml`, `devenv/litellm/config.yaml` and `devenv/mockllm/server.py`.
- [x] Makefile: add `dev-up`, `dev-down` and `dev-logs` (`podman compose -f devenv/compose.yaml …`).
- [x] `.dockerignore`: `dev/` → `devenv/`.
- [ ] Smoke-test: `make dev-up`, confirm the existing `agentd_agentd-data` volume is reused and all four services go healthy.

### 3. `test/`

- [x] `git mv Dockerfile.test test/container/Dockerfile`. Add `test/container/Dockerfile.dockerignore` (root copy, minus test patterns). Fix the header build command.
- [x] ~~`git mv test/e2e/chat-kanban.sh`~~ — superseded: the shell QA script was deleted outright in T-027 (2026-09-30) once J04 and J07 covered its assertions. `test/e2e/` now holds the Go journey suite.
- [x] Makefile: `podman-test` → `podman build -f test/container/Dockerfile …`.
- [x] `.gitignore`: drop `!test/container/Dockerfile`. `.dockerignore`: add `test/`, drop `.e2e/`.

### 4. `tools/`

- [x] `git mv scripts/checkloc scripts/checkminfunc tools/`.
- [x] `git mv scripts/verify_gomod.sh tools/ci/verify_gomod.sh`; `git mv scripts/ci_report_failure.sh tools/ci/report_failure.sh`. Update the call inside `verify_gomod.sh`.
- [x] `git mv scripts/llm-smoke.sh tools/diag/llm-smoke.sh`. Makefile: `smoke-contract` echo → `tools/diag/llm-smoke.sh`.
- [x] `tools/checkminfunc/main.go`: set the `-baseline` default path to `tools/checkminfunc/baseline`. In `baseline.go` and `main_test.go`, change the "Generated by" string.
- [x] Rewrite the baseline's own entries (`scripts/checkminfunc/…` → `tools/checkminfunc/…`) and confirm `make minfunc` passes without `minfunc-accept`.
- [x] Makefile: `loc`, `minfunc` and `minfunc-accept` → `./tools/…`. Remove `folder-audit` and its `.PHONY` entry.
- [ ] `.gitignore`: `scripts/checkminfunc/baseline.bak` → `tools/checkminfunc/baseline.bak`; drop `folder_audit`.

### 5. `e2e/`

- [x] `git mv e2e/http_test.go internal/api/tests/feature/api_http_test.go`. Change the package to `api_test` and drop the four duplicate helpers.
- [x] Makefile: remove `test-e2e` and its `.PHONY` entry (T-026 reintroduces it for the journey suite).

### 6. CI (`.github/workflows/ci-go.yml`)

- [x] `paths` (`push` and `pull_request`): replace `scripts/**` with `tools/**`. `**/*.go` does not cover the `baseline` file.
- [x] `run: ./scripts/verify_gomod.sh` → `./tools/ci/verify_gomod.sh`.
- [x] Min-function step diagnostics: `scripts/checkminfunc/baseline` → `tools/checkminfunc/baseline` (3 lines).

### 7. Docs and agent config

- [x] `TESTING_PLAN.md`: 6 `docker-compose.dev.yml` commands → `podman compose -f devenv/compose.yaml …` (the doc calls compose directly rather than `make dev-*`).
- [x] `README.md`: `Dockerfile.test` paragraph and the `folder_audit` line.
- [x] `CONTRIBUTING.md`, `REVIEW.md`, `.agents/skills/code-review-ready/SKILL.md`: `make test-e2e` → `make test PKG=./internal/api/...`.
- [x] `REVIEW.md`, `GUARDRAILS.md`, `docs/guardrails.md`: `scripts/checkloc` / `scripts/checkminfunc` → `tools/…`.
- [x] [container-development.md](../container-development.md): `Dockerfile.test` → `test/container/Dockerfile` (8 places), plus the no-container steps from `test-env.sh`.
- [x] [litellm-integration.md](../litellm-integration.md), [checkpoints.md](../testing/checkpoints.md), [qa-and-browser-verification.md](../testing/qa-and-browser-verification.md), [troubleshooting.md](../testing/troubleshooting.md), [results.md](../testing/results.md): compose path and `dev/` → `devenv/`; `scripts/chat-kanban-qa.sh` (deleted in T-027, 2026-09-30; the e2e journey suite replaced it).
- [ ] [api-testing.md](../api-testing.md): 7 `e2e/http_test.go:<line>` refs and `make test-e2e`.
- [x] [llm-connector-strategy.md](../llm-connector-strategy.md), [provider-tool-calling.md](../provider-tool-calling.md), [agentic-harness-roadmap.md](../agentic-harness-roadmap.md): `scripts/llm-smoke.sh` → `tools/diag/llm-smoke.sh`.
- [ ] Agent memory note `podman-runtime-target`: `docker-compose.dev.yml` + `dev/` → `devenv/`.
- [ ] Final sweep: `git grep -nE 'docker-compose\.dev|Dockerfile\.(test|init-test)|scripts/(check|verify|ci_|mock_llm|chat-kanban|llm-smoke|test-env|run-tests|integration)|\./e2e|test-e2e|[^/]dev/(agentd|litellm|mockllm)'` returns nothing outside `tasks/`.

T-024 and T-025 have both landed, so the Status above is **implemented**. The
five unticked boxes below are follow-up cleanups, not blockers: they touch
`.gitignore`, `docs/api-testing.md`, an agent memory note, the volume-reuse
smoke test and a final `git grep` sweep.
