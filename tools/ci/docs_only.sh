#!/usr/bin/env bash
# Prints docs_only=true when every file a PR changes is documentation
# (docs/**, any *.md, the Markdown lint and link-check configs), else
# docs_only=false. CI (Go) then skips lint and tests; the docs checks still run.
# Without a base ref (pushes to main) it prints false, so main keeps full proof.
# Usage: docs_only.sh [base-ref]  (default: origin/$GITHUB_BASE_REF on PRs)
set -euo pipefail

base=${1:-${GITHUB_BASE_REF:+origin/$GITHUB_BASE_REF}}
if [[ -z "$base" ]]; then
  echo "docs_only=false"
  exit 0
fi

changed=$(git diff --name-only --no-renames "$base" HEAD)
docs='^docs/|\.md$|^\.markdownlint\.json$|^\.lychee\.toml$'
if [[ -n "$changed" ]] && ! grep -qvE "$docs" <<<"$changed"; then
  echo "docs_only=true"
else
  echo "docs_only=false"
fi
