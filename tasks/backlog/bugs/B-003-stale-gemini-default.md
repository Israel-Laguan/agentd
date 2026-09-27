# B-003: Stale gemini-2.5-flash default survives T-012

| Field | Value |
| --- | --- |
| Type | bug |
| Status | done |
| Priority | P2 |
| Sprint | backlog |
| Severity | minor |
| Links | [S02 retro](../../sprints/S02-harness-reliability/retro/RETRO.md), `internal/config/gateway.go:81`, `config.reference.yaml:44`, `docs/config-reference.md:111` |

## Symptoms

`gemini-2.5-flash` is still the compiled-in default model for the gemini provider, even though S01's SP-003 spike documented it returning 404, and S02's T-012 ("Drop stale gemini-2.5 default") was marked `done`. The default was never actually removed from the runtime path — only from wherever T-012 touched.

## Repro

```
grep -n 'gemini-2.5-flash' internal/config/gateway.go config.reference.yaml docs/config-reference.md
```
All three still show it as of 2026-09-27.

## Expected

`gateway.SetDefault("gateway.gemini.model", ...)` either has no default (forcing explicit config) or points at a model id known to work, matching what T-012's acceptance criteria intended (order-only cascade / valid id or empty model).

## Actual

`internal/config/gateway.go:81` sets `gemini-2.5-flash` as the default; `config.reference.yaml` and `docs/config-reference.md` document the same stale value.

## Notes

- Test files also reference `gemini-2.5-flash` as fixture data — those are fine to leave (they're mocking a model id, not asserting it works against a real provider) and are not part of this bug's scope.
- This has been re-flagged as "unresolved" in the S02 retro itself; it never got its own ticket, which is likely why it was never actually fixed.
