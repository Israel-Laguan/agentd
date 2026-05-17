#!/usr/bin/env bash
# Report a CI failure to stdout, GitHub step summary, and workflow annotations.
# Usage: ci_report_failure.sh <title> <message> [extra markdown lines...]
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: ci_report_failure.sh <title> <message> [markdown body lines...]" >&2
  exit 2
fi

title=$1
message=$2
shift 2

echo "$message"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### $title"
    echo ""
    echo "$message"
    if [[ $# -gt 0 ]]; then
      echo ""
      for line in "$@"; do
        echo "$line"
      done
    fi
    echo ""
  } >>"$GITHUB_STEP_SUMMARY"
fi

if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
  echo "::error title=${title}::${message}"
fi
