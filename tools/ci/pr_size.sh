#!/usr/bin/env bash
# PR size gate: fail at >= MAX_FILES changed files or > MAX_LINES changed lines
# (additions + deletions), unless the PR carries the OVERRIDE_LABEL label.
# Usage: pr_size.sh <changed_files> <additions> <deletions> [comma-separated labels]
# Exit: 0 within limits or overridden, 1 over a limit, 2 bad usage.
set -euo pipefail

MAX_FILES=100
MAX_LINES=1000
OVERRIDE_LABEL=large-pr-ok

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo "usage: pr_size.sh <changed_files> <additions> <deletions> [labels]" >&2
  exit 2
fi

files=$1
additions=$2
deletions=$3
labels=${4:-}

for n in "$files" "$additions" "$deletions"; do
  if [[ ! $n =~ ^[0-9]+$ ]]; then
    echo "usage: pr_size.sh: '$n' is not a non-negative integer" >&2
    exit 2
  fi
done

lines=$((additions + deletions))
echo "PR size: $files files, $lines changed lines (+$additions/-$deletions); limits: <$MAX_FILES files, <=$MAX_LINES lines"

over=()
if ((files >= MAX_FILES)); then over+=("files: $files >= $MAX_FILES"); fi
if ((lines > MAX_LINES)); then over+=("lines: $lines > $MAX_LINES"); fi

if ((${#over[@]} == 0)); then
  exit 0
fi

IFS=',' read -r -a label_list <<<"$labels"
for l in "${label_list[@]}"; do
  if [[ $l == "$OVERRIDE_LABEL" ]]; then
    echo "::notice::PR is over the size limit (${over[*]}) but carries the '$OVERRIDE_LABEL' label; skipping."
    echo "over limit, overridden by label $OVERRIDE_LABEL"
    exit 0
  fi
done

printf '::error::PR too large (%s). Split it, or a maintainer can add the %s label.\n' "${over[*]}" "$OVERRIDE_LABEL"
echo "PR too large (${over[*]}); split it or add the $OVERRIDE_LABEL label"
exit 1
