# B-075: make test and make lint pick up a Go package inside web/node_modules

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P3 |
| Sprint | backlog |
| Severity | minor |
| Links | Makefile:19 (`PKG ?=`), web/package-lock.json (`flatted`), found during B-065 (S16) |

## Symptoms

The npm package `flatted` (a web dev dependency) ships a Go package at `web/node_modules/flatted/golang/pkg/flatted`. Once `web/node_modules` is installed, `go list ./...` lists `agentd/web/node_modules/flatted/golang/pkg/flatted`, and the `PKG` default only filters out `^agentd/(web|docs)$`. So `make test`, `make lint` and `make check` build, test and lint third-party code that is not ours.

## Repro

1. `cd web && npm ci`
2. `go list ./... | grep node_modules` prints the flatted package; `make -n lint` shows it in the golangci-lint package list.

## Expected

`PKG` lists only agentd's own Go packages, whether or not `web/node_modules` is installed.

## Actual

A lint finding or test failure in `flatted` would fail `make check` locally. CI's Go job does not install `web/node_modules`, so CI and local runs check different package sets.

## Notes

- Likely fix: filter `/node_modules/` in the `PKG` default (`grep -vE '^agentd/(web|docs)$$|/node_modules/'`), or list `./cmd/... ./internal/... ./tools/...` explicitly. Check `test-bins` and `coverage`, which reuse `PKG`.
