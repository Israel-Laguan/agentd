#!/bin/bash
# tiered-harness.sh — Cost demo comparing a single-model baseline against the
# tiered pipeline on one fixed task pack.
#
# Usage:
#   ./scripts/demo/tiered-harness.sh [OPTIONS]
#
# Options:
#   --baseline-only      Report only the baseline (single strong model)
#   --tiered-only        Report only the tiered pipeline
#   --task-pack PATH     Task pack JSON (default: fixtures/tiered/task.json)
#   --fixtures DIR       Seeded step responses (default: fixtures/tiered)
#   --output FILE        Write results JSON (default: ./tiered-harness-results.json)
#   --mock               Accepted for compatibility; this harness is always offline
#
# WHAT IS AND IS NOT MEASURED
#   Token counts are derived from the actual byte size of the seeded request and
#   response fixtures, using a documented 4-bytes-per-token proxy. Costs are
#   those token counts against the pricing table below. Nothing is hardcoded:
#   edit a fixture and the numbers move.
#
#   Wall-clock latency is NOT reported. Reading fixtures takes microseconds and
#   says nothing about provider latency, so there is no honest offline number to
#   print. A latency comparison needs a live run against real providers.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

FIXTURE_DIR="${FIXTURE_DIR:-$SCRIPT_DIR/fixtures/tiered}"
OUTPUT_FILE="${OUTPUT_FILE:-./tiered-harness-results.json}"
TASK_PACK="${TASK_PACK:-}"
MODE="${MODE:-both}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --baseline-only) MODE="baseline"; shift ;;
    --tiered-only)   MODE="tiered";   shift ;;
    --task-pack)     TASK_PACK="$2";  shift 2 ;;
    --fixtures)      FIXTURE_DIR="$2"; shift 2 ;;
    --output)        OUTPUT_FILE="$2"; shift 2 ;;
    --mock)          shift ;;
    -h|--help)       sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

TASK_PACK="${TASK_PACK:-$FIXTURE_DIR/task.json}"

case "$MODE" in
  baseline|tiered|both) ;;
  *) echo "invalid MODE: $MODE (expected baseline, tiered, or both)" >&2; exit 1 ;;
esac

# Pricing table (USD per 1k tokens), offline proxy for the three tiers.
PRICE_SMALL=0.001
PRICE_MID=0.005
PRICE_STRONG=0.015

# BYTES_PER_TOKEN is the proxy used to turn fixture size into a token count.
BYTES_PER_TOKEN=4

calc() { awk "BEGIN {printf \"%.4f\", $1}"; }
pct()  { awk "BEGIN {printf \"%.1f\", $1}"; }

# json_escape STRING -> STRING with " and \ escaped for safe interpolation
# into a JSON string literal (paths can otherwise break the results file).
json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  printf '%s' "$s"
}

require_file() {
  if [[ ! -f "$1" ]]; then
    echo "missing fixture: $1" >&2
    exit 1
  fi
}

# tokens_of FILE... -> token count across the given files. Floor division
# with a floor of 1: a real (non-empty) fixture set must never price out at
# zero tokens, which would otherwise divide-by-zero downstream (ratios,
# percentages) and produce -nan/+inf in the results JSON.
tokens_of() {
  local total=0 file
  for file in "$@"; do
    require_file "$file"
    total=$(( total + $(wc -c < "$file") ))
  done
  local tokens=$(( total / BYTES_PER_TOKEN ))
  if (( tokens < 1 )); then
    tokens=1
  fi
  echo "$tokens"
}

cost_of() { calc "$1 * $2 / 1000"; }

require_file "$TASK_PACK"

CONTEXT_OUT="$FIXTURE_DIR/step.context.json"
DECISION_OUT="$FIXTURE_DIR/step.decision.json"
EXECUTE_OUT="$FIXTURE_DIR/step.execute.txt"
VERIFY_OUT="$FIXTURE_DIR/step.verify.json"
BASELINE_OUT="$FIXTURE_DIR/baseline.strong.txt"

# Baseline acceptance criterion: the seeded baseline response must explicitly
# state the implementation is complete on its own result line. The marker is
# anchored to the start of the line (`^Implementation complete:`) so a negated
# or quoted mention ("not Implementation complete", `"Implementation complete"`
# in prose) does not pass. This keeps the baseline arm comparable to the
# tiered arm's structured `overall == pass` verify verdict: both require an
# explicit success signal, not merely "the file is non-empty".
BASELINE_ACCEPTANCE_MARKER="^Implementation complete:"

echo "=== Tiered Execution Cost Harness ==="
echo ""
echo "Task pack:  $TASK_PACK"
echo "Fixtures:   $FIXTURE_DIR"
echo "Token proxy: 1 token per ${BYTES_PER_TOKEN} bytes of seeded request+response"
echo "Pricing/1k:  small=\$$PRICE_SMALL mid=\$$PRICE_MID strong=\$$PRICE_STRONG"
echo ""

BASELINE_JSON=""
TIERED_JSON=""
COMPARISON_JSON=""

if [[ "$MODE" == "baseline" || "$MODE" == "both" ]]; then
  # Baseline: one strong model sees the task and produces everything itself,
  # including the repository exploration captured in its response.
  BASELINE_TOKENS=$(tokens_of "$TASK_PACK" "$BASELINE_OUT")
  BASELINE_COST=$(cost_of "$BASELINE_TOKENS" "$PRICE_STRONG")

  if grep -q -- "$BASELINE_ACCEPTANCE_MARKER" "$BASELINE_OUT"; then
    BASELINE_ACCEPTANCE_PASS="true"
  else
    BASELINE_ACCEPTANCE_PASS="false"
  fi
  if [[ "$BASELINE_ACCEPTANCE_PASS" != "true" ]]; then
    echo "baseline arm did not meet the acceptance criterion (missing marker: $BASELINE_ACCEPTANCE_MARKER)" >&2
    exit 1
  fi

  echo "=== Baseline (single strong model) ==="
  echo "Tokens: $BASELINE_TOKENS"
  echo "Cost:   \$$BASELINE_COST"
  echo "Acceptance: pass (marker found)"
  echo ""

  BASELINE_JSON=$(cat <<JSON
  "baseline": {
    "model_tier": "strong",
    "tokens": $BASELINE_TOKENS,
    "cost_usd": $BASELINE_COST,
    "acceptance_pass": $BASELINE_ACCEPTANCE_PASS
  }
JSON
)
fi

if [[ "$MODE" == "tiered" || "$MODE" == "both" ]]; then
  # Tiered: each step is charged for what it actually reads plus what it writes.
  # The sealed pack is what lets the cheap steps skip re-reading the repository.
  CONTEXT_TOKENS=$(tokens_of "$TASK_PACK" "$CONTEXT_OUT")
  DECISION_TOKENS=$(tokens_of "$TASK_PACK" "$CONTEXT_OUT" "$DECISION_OUT")
  EXECUTE_TOKENS=$(tokens_of "$TASK_PACK" "$CONTEXT_OUT" "$DECISION_OUT" "$EXECUTE_OUT")
  VERIFY_TOKENS=$(tokens_of "$TASK_PACK" "$CONTEXT_OUT" "$DECISION_OUT" "$VERIFY_OUT")

  CONTEXT_COST=$(cost_of "$CONTEXT_TOKENS" "$PRICE_SMALL")
  DECISION_COST=$(cost_of "$DECISION_TOKENS" "$PRICE_MID")
  EXECUTE_COST=$(cost_of "$EXECUTE_TOKENS" "$PRICE_SMALL")
  VERIFY_COST=$(cost_of "$VERIFY_TOKENS" "$PRICE_MID")

  TIERED_TOKENS=$(( CONTEXT_TOKENS + DECISION_TOKENS + EXECUTE_TOKENS + VERIFY_TOKENS ))
  # Sum from unrounded token*price products rather than adding the already
  # (4-decimal) rounded per-step costs, so the aggregate does not accumulate
  # rounding error across steps; only the final total is rounded for display.
  TIERED_COST=$(calc "($CONTEXT_TOKENS * $PRICE_SMALL + $DECISION_TOKENS * $PRICE_MID + $EXECUTE_TOKENS * $PRICE_SMALL + $VERIFY_TOKENS * $PRICE_MID) / 1000")

  # The acceptance check is the seeded verify verdict, not an assertion in this
  # script: both arms have to satisfy the same criterion to be comparable.
  VERIFY_VERDICT=$(awk -F'"' '/"overall"/ {print $4}' "$VERIFY_OUT")
  if [[ "$VERIFY_VERDICT" != "pass" ]]; then
    echo "tiered arm did not meet the acceptance criterion (verify overall=$VERIFY_VERDICT)" >&2
    exit 1
  fi

  echo "=== Tiered (context→decision→execute→verify) ==="
  printf 'context  (small)  %6s tokens  $%s\n' "$CONTEXT_TOKENS" "$CONTEXT_COST"
  printf 'decision (mid)    %6s tokens  $%s\n' "$DECISION_TOKENS" "$DECISION_COST"
  printf 'execute  (small)  %6s tokens  $%s\n' "$EXECUTE_TOKENS" "$EXECUTE_COST"
  printf 'verify   (mid)    %6s tokens  $%s\n' "$VERIFY_TOKENS" "$VERIFY_COST"
  echo "Tokens: $TIERED_TOKENS"
  echo "Cost:   \$$TIERED_COST"
  echo "Acceptance: verify overall=$VERIFY_VERDICT"
  echo ""

  VERIFY_VERDICT_ESCAPED=$(json_escape "$VERIFY_VERDICT")
  TIERED_JSON=$(cat <<JSON
  "tiered": {
    "steps": {
      "context":  {"model_tier": "small", "tokens": $CONTEXT_TOKENS,  "cost_usd": $CONTEXT_COST},
      "decision": {"model_tier": "mid",   "tokens": $DECISION_TOKENS, "cost_usd": $DECISION_COST},
      "execute":  {"model_tier": "small", "tokens": $EXECUTE_TOKENS,  "cost_usd": $EXECUTE_COST},
      "verify":   {"model_tier": "mid",   "tokens": $VERIFY_TOKENS,   "cost_usd": $VERIFY_COST}
    },
    "total_tokens": $TIERED_TOKENS,
    "total_cost_usd": $TIERED_COST,
    "acceptance_pass": true,
    "verify_overall": "$VERIFY_VERDICT_ESCAPED"
  }
JSON
)
fi

if [[ "$MODE" == "both" ]]; then
  COST_SAVED=$(calc "$BASELINE_COST - $TIERED_COST")
  COST_REDUCTION=$(pct "($BASELINE_COST - $TIERED_COST) / $BASELINE_COST * 100")
  # Tokens go UP, not down: every step re-reads the sealed pack, so the pipeline
  # buys its cost win with extra tokens on cheaper tiers. Reported as a ratio
  # rather than a "reduction" so the sign cannot be mistaken for a saving.
  TOKEN_RATIO=$(awk "BEGIN {printf \"%.2f\", $TIERED_TOKENS / $BASELINE_TOKENS}")

  echo "=== Comparison ==="
  echo "Tokens:  $BASELINE_TOKENS -> $TIERED_TOKENS (${TOKEN_RATIO}x MORE)"
  echo "Cost:    \$$BASELINE_COST -> \$$TIERED_COST (${COST_REDUCTION}% less)"
  echo "Saved:   \$$COST_SAVED per task"
  echo ""
  echo "Read this carefully: the tiered pipeline spends MORE tokens, not fewer."
  echo "Every step re-reads the sealed pack, so total tokens go up. The saving"
  echo "comes entirely from tier assignment — cheap-tier steps are priced at"
  echo "small/mid rates regardless of how large they are, not from any step"
  echo "being token-efficient."
  echo "Tiered execution is a price-per-token play, not a token-efficiency play."
  echo ""
  echo "Wall-clock latency is not reported — see the header for why."
  echo ""

  COMPARISON_JSON=$(cat <<JSON
  "comparison": {
    "cost_saved_usd": $COST_SAVED,
    "cost_reduction_percent": $COST_REDUCTION,
    "tokens_note": "tiered spends more tokens than the baseline; the saving is price per token, not token count",
    "token_ratio_vs_baseline": $TOKEN_RATIO
  }
JSON
)
fi

if [[ -n "$OUTPUT_FILE" ]]; then
  SECTIONS=()
  [[ -n "$BASELINE_JSON" ]] && SECTIONS+=("$BASELINE_JSON")
  [[ -n "$TIERED_JSON" ]] && SECTIONS+=("$TIERED_JSON")
  [[ -n "$COMPARISON_JSON" ]] && SECTIONS+=("$COMPARISON_JSON")

  BODY=""
  first=1
  for section in "${SECTIONS[@]}"; do
    if [[ $first -eq 1 ]]; then
      BODY="$section"
      first=0
    else
      BODY="$BODY,
$section"
    fi
  done

  TASK_PACK_ESCAPED=$(json_escape "$TASK_PACK")
  FIXTURE_DIR_ESCAPED=$(json_escape "$FIXTURE_DIR")

  cat > "$OUTPUT_FILE" <<RESULTS
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "mode": "offline-fixture",
  "note": "Token counts derived from seeded fixture sizes at ${BYTES_PER_TOKEN} bytes/token; costs from the pricing table. Wall-clock latency is not measured offline and is intentionally absent.",
  "task_pack": "$TASK_PACK_ESCAPED",
  "fixtures": "$FIXTURE_DIR_ESCAPED",
  "bytes_per_token": $BYTES_PER_TOKEN,
  "pricing_per_1k_usd": {"small": $PRICE_SMALL, "mid": $PRICE_MID, "strong": $PRICE_STRONG},
$BODY
}
RESULTS
  echo "Results written to: $OUTPUT_FILE"
fi
