# Reviewing pull requests

Playbook for reviewers checking a PR branch before approving. Authors should follow [`CONTRIBUTING.md`](CONTRIBUTING.md); autonomous agents follow [`GUARDRAILS.md`](GUARDRAILS.md).

## Prerequisites

- Go 1.26+
- GNU Make
- [`golangci-lint`](https://golangci-lint.run/) at `$(go env GOPATH)/bin/golangci-lint` (same path the Makefile uses)
- For PRs that touch `web/`: Node.js and `npm ci` in `web/`

## Checkout the PR branch

```sh
git fetch origin
git checkout <pr-branch>
```

## Verification order

Run these on the PR branch. Order is fastest-fail first; `make check` is the required merge gate.

| Step | Command | What it proves |
| --- | --- | --- |
| Deps | `make tidy` | `go.mod` / `go.sum` are consistent |
| Compile | `make build` | `cmd/agentd` builds to `bin/agentd` |
| LOC | `make loc` | File size limits ([`scripts/check_loc.py`](scripts/check_loc.py): 300 default, 500 for `*_test.go`, 400 under `docs/`) |
| Lint | `make lint` | `golangci-lint` + [`depguard`](.golangci.yml) architecture rules |
| Tests | `make test` | Race-enabled tests for `./...` |
| Full gate | `make check` | `loc` + `lint` + `test` (required before merge) |
| E2E (if API/queue touched) | `make test-e2e` | `./e2e/...` |
| Web (if `web/` changed) | `cd web && npm run build && npm run lint` | Next.js build + ESLint |

### Targeted checks (large PRs)

```sh
go test -race ./internal/queue/worker/...
go test -race -run TestSpecific ./path/to/pkg
```

Prefer `make test` (or `make check`) so the workspace-local Go cache is used; see below if you run `go test` directly.

## Go build cache troubleshooting

`make build`, `make lint`, `make test`, and `make coverage` all set `GOCACHE=$(pwd)/.gocache` via the Makefile. That avoids stale artefacts from the **global** Go cache after `git checkout` or when switching branches.

### Symptoms

- `make lint` or `golangci-lint` fails locally but passes elsewhere (or on a fresh clone)
- Phantom compile or type-check errors that disappear after a clean build
- Inconsistent results between `make test` and a bare `go test`

### Fixes (try in order)

1. **Use Makefile targets** — `make lint`, `make build`, `make test`, or `make check` (all use `.gocache/`).
2. **Reset workspace cache** — `rm -rf .gocache` then `make check`.
3. **Manual override** (if you must run tools outside Make):

   ```sh
   env GOCACHE=$(pwd)/.gocache golangci-lint run ./...
   env GOCACHE=$(pwd)/.gocache go test -race ./...
   ```

4. **Last resort** — `go clean -cache` clears the **global** cache and affects all Go projects on your machine.

`.gocache/` is listed in [`.gitignore`](.gitignore) and is safe to delete anytime.

## Code review checklist

Mechanical checks above are necessary but not sufficient. Also verify:

- **Architecture / layers** — [`docs/architecture.md`](docs/architecture.md), [`docs/guardrails.md`](docs/guardrails.md); `internal/models` must not import outward packages (`depguard`).
- **Size and complexity** — [`GUARDRAILS.md`](GUARDRAILS.md) Signs #2–#3: file/function limits; no new blanket `//nolint` for `funlen`, `cyclop`, `depguard`, or `revive`.
- **Style** — [`STYLEGUIDE.md`](STYLEGUIDE.md).
- **Tests** — Behavior changes include tests; BDD flows use `*.feature` plus `*_feature_steps_test.go`.
- **PR scope** — One concern per PR; description explains *why*, not only *what*.

## What “green” means

- `make check` exits 0 with no warnings on the PR branch.
- If `web/` changed: `npm run build` and `npm run lint` pass in `web/`.
- You have read the diff for the invariants above, not only run the commands.
