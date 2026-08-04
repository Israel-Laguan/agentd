GO ?= go
GOLANGCI_LINT ?= $(shell $(GO) env GOPATH)/bin/golangci-lint
# Comma-separated patterns for merged coverage (default: entire module). Override to narrow the denominator, e.g. internal-only: $(shell go list ./internal/... | paste -sd, -)
COVERPKG ?= ./...

.PHONY: build test coverage run tidy lint lint-install loc minfunc folder-audit check test-e2e podman-test lint-md lint-links lint-docs

# Workspace-local GOCACHE; default GOMODCACHE to the user module cache (agent
# sandboxes often set an empty GOMODCACHE and break go test / make build).
GOMODCACHE ?= $(HOME)/go/pkg/mod
# Workspace-local GOCACHE for all compile/lint/test paths to avoid stale-build
# artefacts when switching branches or when the global cache becomes inconsistent.
GO_ENV = env GOCACHE=$(CURDIR)/.gocache GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=go1.26.2+auto

test-e2e:
	$(GO_ENV) $(GO) test -v ./e2e/...

build:
	$(GO_ENV) $(GO) build -o bin/agentd ./cmd/agentd

# Scoped runs: make test PKG=./internal/api/controllers/... RUN=TestGateway
PKG ?= $(shell go list ./... | grep -vE '^agentd/(web|docs)$$' | sed 's|^agentd/|./|')
RUN ?=
TEST_FLAGS = -v -race -cover
ifneq ($(strip $(RUN)),)
TEST_FLAGS += -run $(RUN)
endif

test:
	$(GO_ENV) $(GO) test $(TEST_FLAGS) $(PKG)

coverage:
	$(GO_ENV) $(GO) test -v -race -covermode=atomic -coverpkg=$(COVERPKG) -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -html=coverage.out

run:
	$(GO) run ./cmd/agentd

tidy:
	$(GO) mod tidy

lint:
	$(GO_ENV) $(GOLANGCI_LINT) run $(PKG)

lint-install:
	$(GO) install -v github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

podman-test:
	podman build -f Dockerfile.test -t agentd-test .
	podman run --rm agentd-test

loc:
	$(GO) run ./scripts/checkloc --max-lines 300

minfunc:
	$(GO) run ./scripts/checkminfunc --min-lines 3

folder-audit:
	$(GO) run ./scripts/folder_audit --out /tmp/folder-size-audit.md

# Docs linting. Dependencies: `npm install` (markdownlint-cli2 via the root
# package.json script) and `lychee` (single binary, see .lychee.toml). These
# cover docs/** plus root-level guides. The file set for Markdown linting lives
# in package.json's `lint:md` script (single source of truth), invoked here.
LYCHEE ?= lychee

lint-md:
	npm run lint:md

lint-links:
	$(LYCHEE) --config .lychee.toml README.md docs CONTRIBUTING.md GUARDRAILS.md REVIEW.md STYLEGUIDE.md

lint-docs: lint-md lint-links

# check is the required merge gate: loc + minfunc + lint + test + lint-docs
# (Markdown and link checks). Run before pushing.
check: loc minfunc lint test lint-docs
