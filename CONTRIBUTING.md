# Contributing to agentd

## Getting Started

### Prerequisites

- Go 1.26+
- GNU Make
- SQLite (for development; the project uses `modernc.org/sqlite` which bundles a pure-Go driver)

### Setup

```sh
git clone <repo-url>
cd agentd
make tidy        # download dependencies
make check       # run full quality gate (loc + lint + test)
```

## Development Workflow

1. Create a feature branch: `git checkout -b feat/your-feature` or `fix/your-fix`.
2. Make your changes. Write tests alongside behavior changes.
3. Run `make check` locally before pushing.
4. Open a PR with a clear description of the change and any architectural decisions.

## Make Targets

| Target | Description |
| --- | --- |
| `make build` | Compile the binary to `bin/agentd` |
| `make test` | Run race-enabled tests for all packages |
| `make coverage` | Run tests with coverage report |
| `make lint` | Run `golangci-lint` (includes `depguard`, `cyclop`, `funlen`, `revive`) |
| `make loc` | Check file line counts (max 300) |
| `make check` | `loc` + `lint` + `test` (full quality gate) |
| `make tidy` | Run `go mod tidy` |

## Code Standards

- Read [`docs/guardrails.md`](docs/guardrails.md) for size limits, layer boundaries, and the pre-commit checklist.
- Read [`STYLEGUIDE.md`](STYLEGUIDE.md) for coding conventions and style expectations.
- Read [`docs/architecture.md`](docs/architecture.md) for system invariants and data flows.

## Testing

- Unit tests: `*_test.go` files alongside the code they test. Use table-driven tests for multi-case scenarios.
- BDD tests: `*.feature` files with Godog step definitions in `*_feature_steps_test.go` files.
- All tests must pass with the race detector enabled (`go test -race`).

## Pull Request Guidelines

- Keep PRs focused. One concern per PR.
- Describe the "why" in the PR body, not just the "what."
- Reference any relevant architectural invariants or guardrails affected by the change.
- Ensure `make check` passes on the PR branch.
