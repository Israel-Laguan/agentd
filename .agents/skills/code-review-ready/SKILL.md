---
name: code-review-ready
description: Commands and checklist for preparing code before opening or updating a PR, including Makefile targets and verification gates.
---
# Code Review Readiness

Before opening or updating a PR, follow [`REVIEW.md`](REVIEW.md) and [`CONTRIBUTING.md`](CONTRIBUTING.md). **CI merge gate:** `make check` (`loc` + `lint` + `test`).

## Run Go commands reliably

**Prefer Makefile targets** — and in agent/sandbox shells, pin `GOMODCACHE` first so an inherited empty cache cannot override Make defaults.

```sh
cd "$(git rev-parse --show-toplevel)"
export GOMODCACHE="${HOME}/go/pkg/mod"
export GOCACHE="$(pwd)/.gocache"
go mod download
make test PKG=./internal/api/controllers/...   # fast — see below
make check                                     # full gate before push
```

Bare `go test` outside Make? Use the same env: `GOMODCACHE="$HOME/go/pkg/mod"` and `GOCACHE=$(pwd)/.gocache`.

If modules still look broken: re-export `GOMODCACHE="$HOME/go/pkg/mod"` (no fallback/default expression), run `go mod download`, then retry. Stale builds or env mismatches: [`REVIEW.md`](REVIEW.md#go-toolchain-troubleshooting) or `rm -rf .gocache && make check`.

## Fast path (while iterating)

Match scope to the diff — don't run the full repo on every edit.

| Change | Command |
| --- | --- |
| One package / test file | `make test PKG=./internal/api/controllers/...` |
| One test | `make test PKG=./internal/api/controllers/... RUN=TestGatewayListNoModel` |
| `go.mod` / imports | `make tidy && make build` |
| Ready to push / update PR | `make check` |

`make test` accepts optional `PKG` (package path) and `RUN` (test name regex). Examples:

```sh
make test PKG=./internal/api/controllers/... RUN=TestGateway
make test PKG=./internal/queue/worker/...
```

Full `make test` (no `PKG`) runs **race** on `./...` — slower but what CI runs. Use scoped `PKG`/`RUN` while editing; run `make check` before you're done.

## Full verification (before PR)

Fastest-fail order:

```sh
make tidy    # go.mod / go.sum consistent
make build   # bin/agentd
make check   # loc + lint + test — must exit 0
```

Add when the diff touches those areas:

```sh
make test-e2e                              # API or queue behavior
cd web && npm ci && npm run lint && npm run build   # web/ changes
make test PKG=./internal/queue/worker/...  # large worker-only PRs
```

## What each gate enforces

| Target | Enforces |
| --- | --- |
| `make loc` | 300 lines (500 `*_test.go`, 400 `docs/**`) — [`docs/guardrails.md`](docs/guardrails.md) |
| `make lint` | `golangci-lint`, `depguard` layer rules, `funlen`/`cyclop`/`revive` — [`.golangci.yml`](.golangci.yml) |
| `make test` | Race-enabled tests (`PKG` / `RUN` optional) |

## Pre-submit checklist (mechanical + semantic)

From [`docs/guardrails.md`](docs/guardrails.md) and [`GUARDRAILS.md`](GUARDRAILS.md):

- [ ] `make check` passes with zero warnings
- [ ] Behavior changes include tests (`*_test.go`; BDD: `*.feature` + `*_feature_steps_test.go`)
- [ ] No new blanket `//nolint` (especially `funlen`, `cyclop`, `depguard`, `revive`)
- [ ] `internal/models` has no outward imports
- [ ] PR is one concern; description explains **why**, not only **what**

## Style and architecture (read the diff, not only green CI)

Follow [`STYLEGUIDE.md`](STYLEGUIDE.md) and [`docs/architecture.md`](docs/architecture.md):

- `context.Context` first on I/O methods; sentinels in `errors.go` with `%w` wrapping
- Compile-time checks: `var _ Interface = (*Impl)(nil)`
- Import order: stdlib → external → `agentd/internal/...`
- New `internal/` packages need `doc.go`

When structural changes cross package boundaries, read [`GUARDRAILS.md`](GUARDRAILS.md) SIGN #10 before coding.