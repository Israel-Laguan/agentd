GO ?= go
GOLANGCI_LINT ?= $(shell $(GO) env GOPATH)/bin/golangci-lint
# Comma-separated patterns for merged coverage (default: entire module). Override to narrow the denominator, e.g. internal-only: $(shell go list ./internal/... | paste -sd, -)
COVERPKG ?= ./...

.PHONY: build test coverage run tidy lint loc check test-e2e

test-e2e:
	$(GO) test -v ./e2e/...

build:
	$(GO) build -o bin/agentd ./cmd/agentd

# Use a workspace-local GOCACHE to avoid stale-build artefacts when switching branches
# or when the global cache becomes inconsistent, without sharing /tmp across concurrent jobs.
TEST_ENV = env GOCACHE=$(CURDIR)/.gocache

test:
	$(TEST_ENV) $(GO) test -v -race -cover ./...

coverage:
	$(TEST_ENV) $(GO) test -v -race -covermode=atomic -coverpkg=$(COVERPKG) -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out

run:
	$(GO) run ./cmd/agentd

tidy:
	$(GO) mod tidy

lint:
	$(GOLANGCI_LINT) run ./...

loc:
	python3 ./scripts/check_loc.py --max-lines 300

check: loc lint test
