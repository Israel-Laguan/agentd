#!/usr/bin/env bash
# PR size gate: fail at >= MAX_FILES changed files or > MAX_LINES changed lines
# in non-generated files, unless OVERRIDE_LABEL is present.
#
# The "changed lines" number is computed from the REAL diff between the PR's
# base and head commits (git diff --numstat), NOT from GitHub's aggregated
# additions/deletions totals. GitHub's totals count every line of generated /
# lockfile churn one-for-one, which makes deletion-heavy PRs (dependency drops,
# lockfile regeneration) look far larger than the human-reviewed code they
# actually contain. Counting real code lines keeps review cost in line with
# what a human reviewer would actually read.
#
# Supply the real diff via git, or fall back to the legacy totals interface
# (github event fields) for local/CI-fallback runs.
#
# Usage:
#   With git (CI): pr_size.sh <base_sha> <head_sha> [labels]
#   Legacy (local): pr_size.sh <changed_files> <additions> <deletions> [labels]
# Exit: 0 within limits or overridden, 1 over a limit, 2 bad usage.
set -euo pipefail

MAX_FILES=100
MAX_LINES=1000
OVERRIDE_LABEL=large-pr-ok

# Basenames / path globs excluded from the size accounting. These are generated
# or machine-produced files whose churn carries no human review signal
# (lockfiles regenerated on every dependency change, minified assets, build
# caches). Organic source files are never excluded.
EXCLUDE_PATTERNS=(
  'package-lock.json'   # npm lockfile, regenerated
  'pnpm-lock.yaml'      # pnpm lockfile, regenerated
  'yarn.lock'           # yarn lockfile, regenerated
  'Cargo.lock'          # cargo lockfile, regenerated
  'go.sum'              # go module checksum, mechanical
  'composer.lock'       # composer lockfile
  'Gemfile.lock'        # bundler lockfile
  'poetry.lock'         # poetry lockfile
  '*.min.js'            # minified assets
  '*.min.css'           # minified assets
  '*.css.map'           # source maps
  '*.tsbuildinfo'       # typescript build info
)

# --- Argument parsing ---------------------------------------------------

if [[ -n "${PR_GIT:-}" ]]; then
  # Git mode (CI): pr_size.sh <base_sha> <head_sha> [labels]
  base=$1
  head=$2
  labels=${3:-}
  legacy=0
else
  # Legacy totals mode (local/CI fallback): pr_size.sh <files> <additions> <deletions> [labels]
  if [[ $# -lt 3 ]]; then
    echo "usage: pr_size.sh <files> <additions> <deletions> [labels]" >&2
    exit 2
  fi
  files=$1
  additions=$2
  deletions=$3
  labels=${4:-}
  base=
  head=
  legacy=1
fi

if [[ $legacy -eq 1 ]]; then
  for n in "$files" "$additions" "$deletions"; do
    if [[ ! $n =~ ^[0-9]+$ ]]; then
      echo "usage: pr_size.sh: '$n' is not a non-negative integer" >&2
      exit 2
    fi
  done
fi

# --- Size computation ---------------------------------------------------

excluded_files=0

if [[ $legacy -eq 0 ]]; then
  if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
    echo "error: PR_GIT mode requires a git checkout" >&2
    exit 2
  fi

  # Both endpoints must exist locally. In CI this fails with a shallow
  # checkout (only the head commit is fetched); fix with `fetch-depth: 0`.
  for rev in "$base" "$head"; do
    if ! git cat-file -e "$rev" 2>/dev/null; then
      echo "error: git object '$rev' not found locally." >&2
      echo "hint: fetch full history (actions/checkout fetch-depth: 0), or pass legacy totals via legacy mode" >&2
      exit 2
    fi
  done

  files=0
  lines=0

  if [[ -z "$(git diff --numstat "$base" "$head" | head -1)" && -z "${PR_FALLBACK:-}" ]]; then
    echo "error: git diff returned nothing; pass legacy totals via legacy mode" >&2
    exit 2
  fi

  while IFS=$'\t' read -r added deleted file; do
    [[ "$added" == "-" || "$deleted" == "-" ]] && continue
    # Skip files whose churn is excluded (their lines do not count).
    matched=0
    for pat in "${EXCLUDE_PATTERNS[@]}"; do
      if [[ "$file" == $pat ]]; then
        matched=1
        break
      fi
    done
    if [[ $matched -eq 1 ]]; then
      excluded_files=$((excluded_files + 1))
      continue
    fi
    files=$((files + 1))
    lines=$((lines + added + deleted))
  done < <(git diff --numstat "$base" "$head")

  echo "PR size (git diff $base..$head): $files files, $lines changed lines; excluded $excluded_files generated files (<$MAX_FILES files, <=$MAX_LINES lines)"
else
  lines=$((additions + deletions))
  echo "PR size (legacy totals): $files files, $lines changed lines (+$additions/-$deletions); limits: <$MAX_FILES files, <=$MAX_LINES lines"
fi

# --- Limit checks ------------------------------------------------------

over=()
if ((files >= MAX_FILES)); then over+=("files: $files >= $MAX_FILES"); fi
if ((lines > MAX_LINES)); then over+=("lines: $lines > $MAX_LINES"); fi

if ((${#over[@]} == 0)); then
  exit 0
fi

IFS=',' read -r -a label_list <<<"$labels"
for l in "${label_list[@]:-}"; do
  if [[ $l == "$OVERRIDE_LABEL" ]]; then
    echo "::notice::PR is over the size limit (${over[*]}) but carries the '$OVERRIDE_LABEL' label; skipping."
    echo "over limit, overridden by label $OVERRIDE_LABEL"
    exit 0
  fi
done

printf '::error::PR too large (%s). Split it, or a maintainer can add the %s label.\n' "${over[*]}" "$OVERRIDE_LABEL"
echo "PR too large (${over[*]}); split it or add the $OVERRIDE_LABEL label"
exit 1
