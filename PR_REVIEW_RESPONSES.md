# PR review responses

Copy-paste these as threaded replies on the review comments.

---

## 1. Info: `AgenticTruncationThreshold` removal is safe

Acknowledged. Removed the redundant `truncationThreshold` check; `withinLimits` in `AgenticTruncator.Apply` preserves the same early-exit behavior (message count + character budget).

---

## 2. High: `totalChars` undefined in `worker_agentic_truncation_test.go`

`totalChars` lives in `internal/queue/worker/context_budget.go` (package `worker`), so the truncation test can call it without an import. Verified with:

```bash
make test PKG=./internal/queue/worker/... RUN=TestApplyAgenticTruncation
```

---

## 3. Medium: Agentic truncator instantiated every iteration

`AgenticTruncator.Apply` returns immediately via `withinLimits` for short histories; the per-iteration alloc is a small struct plus one O(n) scan. Acceptable unless profiling shows otherwise; we can add an inline early return later if needed.

---

## 4. P2: `SkipTruncation: true` bypasses router truncation

Valid that `agentic_character_budget: 0` plus `SkipTruncation` skipped router safety for character volume.

We keep `SkipTruncation: true` so router head/tail truncation does not break tool-call pairing (agentic truncator is tool-aware).

**Fix applied:** when `queue.agentic_character_budget` is `0`, daemon startup inherits `gateway.truncator.max_input_chars` via `config.EffectiveAgenticCharacterBudget` in `buildWorker` (`cmd/agentd/start.go`). `SkipTruncation` remains `true`.

---

## 5. P2: `docs/api-testing.md` test coverage placement

Test coverage for `GET /api/v1/agents/{id}` is already under that section (lines 237–240), before POST/PATCH. No doc change needed.

---

## 6. P2: `gateway.role_models` not applied to router

`role_models` is loaded via `loadRoleModels` and applied at daemon startup in `cmd/agentd/wiring.go` through `GatewayConfig.RoleRoutes()` → `Router.WithRoleRouting`. Covered by `TestGatewayConfig_RoleRoutes` and gateway role routing tests.

---

## 7. P2: `worker_agentic_integration_test.go` exceeds 500 lines

File is 426 lines; `go run ./scripts/checkloc` reports no violation. Will split helpers into a sibling `*_test.go` if new tests push past 500.
