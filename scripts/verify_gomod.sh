#!/usr/bin/env bash
# Verify go.mod and go.sum match what "go mod tidy" would produce.
set -euo pipefail

make tidy
if ! git diff --quiet go.mod go.sum; then
  echo "::error title=go.mod not tidy::Run 'make tidy' locally and commit go.mod and go.sum"
  git --no-pager diff go.mod go.sum
  exit 1
fi
