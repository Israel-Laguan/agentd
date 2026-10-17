#!/usr/bin/env bash
# Gives every tracked file the time of the last commit that touched it as its
# mtime. go test keys the files a test opens on size + mtime, not content, so
# after a fresh checkout (every mtime = now) those packages miss the test cache.
# With a base ref (PRs), files that differ from it are then touched to now:
# their newest commit can be older than the base's own change to the same file,
# and an equal size would then reuse the base's result.
# Usage: restore_mtimes.sh [base-ref]  (default: origin/$GITHUB_BASE_REF on PRs)
set -euo pipefail

git -c core.quotePath=false log --format='@@%ct' --name-only --no-renames HEAD |
  awk '/^@@/ { t = substr($0, 3); next } NF && !seen[$0]++ { print t "\t" $0 }' |
  while IFS=$'\t' read -r t f; do
    if [[ -f "$f" ]]; then touch -m -d "@$t" -- "$f"; fi
  done

base=${1:-${GITHUB_BASE_REF:+origin/$GITHUB_BASE_REF}}
if [[ -n "$base" ]]; then
  git -c core.quotePath=false diff --name-only --no-renames "$base" HEAD |
    while IFS= read -r f; do
      if [[ -f "$f" ]]; then touch -m -- "$f"; fi
    done
fi
