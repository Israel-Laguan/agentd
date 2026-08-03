# Container-based development with Podman

Use the container images in this repo to get **containerized builds, tests, and lint**
without installing the Go 1.26 toolchain, sqlite libs, or golangci-lint locally.
Runtime commands run in an Alpine-based image; Podman is docker-compatible.

## Prerequisites

- Podman (or Docker) installed and running.
- Network access for the initial image pull and module download.

## Images

| Dockerfile | Purpose | Default user |
|---|---|---|
| `Dockerfile` | Runtime image (`agentd` binary + sqlite-libs + bash) | `agentd` (non-root) |
| `Dockerfile.test` | Test/lint image (Go toolchain + sqlite-libs + bash) | `tester` (non-root) |

### Why a non-root test image matters

Several tests verify that the daemon correctly rejects operations when a data
directory is not writable. A root user bypasses Unix permission bits, so those
tests silently succeed when they should fail. `Dockerfile.test` creates a
dedicated `tester` user and runs `go test` as that user, matching the project's
runtime user conventions.

### Why the test image installs `bash`

The `Dockerfile.test` image also installs `bash` (not just `sqlite-libs`). The
sandbox `BashExecutor` and the plugin pre/post hooks shell out to `/bin/bash`,
so tests such as `TestBashExecutor*`, `TestShellPreHook_*`, and the
`cmd/agentd` non-writable-directory checks fail with
`exec: "bash": executable file not found in $PATH` when `bash` is missing.
Alpine's base `golang` image does not ship `bash` by default, so it must be
installed explicitly. Do not remove `bash` from the `apk add` line unless those
tests have been reworked to use `sh`.

## Build

```bash
# Runtime image (produces /usr/local/bin/agentd inside the image)
podman build -t agentd .

# Verify the binary
podman run --rm agentd --version   # or: agentd --help
```

## Test

```bash
# One-shot: build the test image and run the full suite
make podman-test

# Equivalent manual commands
podman build -f Dockerfile.test -t agentd-test .
podman run --rm agentd-test

# Run a single package (the image caches modules between runs)
podman run --rm agentd-test go test -v ./internal/kanban/
podman run --rm agentd-test go test -v ./internal/queue/worker/
podman run --rm agentd-test go test -v -run TestCLIFeatures ./cmd/agentd/
```

### Why the test image uses `GOTOOLCHAIN=local`

`go.mod` declares `go 1.26.2` while the base image ships `go1.26.5`. Setting
`GOTOOLCHAIN=local` prevents Go from attempting a network toolchain download and
just uses the toolchain already in the image.

## Lint

The project's `.golangci.yml` uses `version: "2"`, which requires **golangci-lint
v2** (CI pins `v2.12.2`). Older v1 releases cannot parse the config and do not
support Go 1.26.

```bash
# Install the correct linter version
make lint-install

# Run lint
make lint

# Or run lint inside the test container (no local install needed)
podman build -f Dockerfile.test -t agentd-test .
podman run --rm agentd-test sh -c '
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 &&
  $(go env GOPATH)/bin/golangci-lint run $(go list ./... | grep -vE "^agentd/(web|docs)$" | sed "s|^agentd/|./|")
'
```

## Quick reference

```bash
make build          # go build -o bin/agentd ./cmd/agentd (uses local Go)
make test           # go test ./... (requires non-root for permission tests)
make lint-install   # install golangci-lint v2.12.2 locally
make lint           # run golangci-lint v2
make podman-test    # build Dockerfile.test and run go test as non-root
podman build -t agentd .            # build runtime image
podman run --rm agentd init         # run agentd init
podman run --rm agentd start -v     # run agentd start (verbose)
```

## Troubleshooting

### `exec: "bash": executable file not found in $PATH`

The sandbox `BashExecutor`, plugin hooks, and some `cmd/agentd` permission
checks run commands through `/bin/bash`. If you rebuilt the test image from a
`Dockerfile.test` that omits `bash` (or swapped the base image to one without
it), those tests fail with this error. Rebuild from the committed `Dockerfile.test`,
which installs `bash` via `apk add --no-cache sqlite-libs bash`.

### `go: downloading go1.26.2 ... no such file or directory`

You are hitting the toolchain auto-downloader. Make sure `GOTOOLCHAIN=local` is
set (it is baked into `Dockerfile.test`). If you override it, set it back:

```bash
podman run --rm -e GOTOOLCHAIN=local agentd-test go test ./...
```

### `unknown field ID in struct literal of type models.Project`

`Project` embeds `BaseEntity`; use the explicit embedded form:

```go
models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp"}
```

This is the pattern used throughout the existing tests.

### `fakeCapabilityAdapter redeclared in this block`

The worker test package already declares `fakeCapabilityAdapter` in
`worker_agentic_mocks_test.go`. New test files in that package must use a
different name.
