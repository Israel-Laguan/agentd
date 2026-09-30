# B-007: devenv builds the same Dockerfile once per agentd service (6 Go compiles per dev-up)

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P3 |
| Sprint | S07-e2e-journeys |
| Severity | minor |
| Links | devenv/compose.yaml, Makefile (dev-up), Dockerfile |

## Symptoms

`make dev-up` runs `podman compose ... up --build -d`, which rebuilds the
agentd image once per service. Every agentd variant (`agentd`, `agentd-healing`,
`agentd-faults`, `agentd-disk`, `agentd-brk`, `agentd-tiered`) declares the same
`build.context: ..` and the same `Dockerfile`, but podman-compose tags a
separate image per service (`agentd_agentd`, `agentd_agentd-healing`, …), so the
expensive builder step runs N times instead of once.

The Dockerfile's only costly step is the Go compile:

```dockerfile
# Dockerfile:8,13
FROM golang:1.26-alpine AS builder
RUN CGO_ENABLED=0 go build -o /agentd ./cmd/agentd
```

Evidence (housekeeping cycle, 2026-09-30): a single `make dev-clean dev-up` on
a tree with edited Go source issued **6** `CGO_ENABLED=0 go build` invocations —
`grep -c "CGO_ENABLED=0 go build" <dev-up log>` = 6 — one per service image, and
only one image had committed when the run was interrupted. Because the Go build
cache is keyed on the source, any edit to `internal/` invalidates it for **all
six** images, so every `dev-up` after a code change pays six full compiles.

## Impact

`dev-up` is the entry point for `make test-e2e` and for every manual run, so the
six-fold compile is paid constantly, not just in CI. It is the dominant
wall-clock cost of bringing the stack up after any Go change.

## Notes

- The count rose from 5 to 6 in this cycle: adding the `tiered` profile to the
  Makefile's `COMPOSE_PROFILES` (needed so `make dev-up` starts agentd-tiered
  for J12) added a sixth service that shares the same build.
- This is a pre-existing compose wart that the `tiered` addition amplified, not
  a regression introduced by it.
- Not fixed in this PR (out of scope; recorded so the cost is on the record).

## Candidate fixes

1. **One shared prebuilt image.** Build `agentd` once (e.g. `make dev-build`
   → `podman build -t agentd:local .`) and have every service use
   `image: agentd:local` with no `build:` block. One compile per dev-up.
2. **`build:` on one service, `image:` on the rest.** Give a single service the
   `build:` block and point the other five at the same tag, so podman-compose
   builds once and reuses the tag.
3. **`make dev-build` before `up`.** Split the build out of `dev-up` so the
   compile is explicit and cacheable, and let `dev-up` reuse the image.

Any of these collapses six compiles to one; option 1 is the simplest.
