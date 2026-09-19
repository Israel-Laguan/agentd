#!/bin/bash
# tiered-harness.sh — Cost/latency demo comparing single-model baseline vs tiered execution
#
# Reads seeded mock-LLM fixture responses per step (scripts/demo/fixtures/*.json,
# or --fixtures-dir override) and derives every token/cost/time total from
# them — no hardcoded aggregate constants. This is a mock/offline harness:
# it never calls a real provider, so its numbers are labeled "offline proxy"
# throughout and must never be reported as measured production costs.
#
# Usage:
#   ./scripts/demo/tiered-harness.sh [OPTIONS]
#
# Options:
#   --baseline-only      Run only the baseline (single mid/strong model)
#   --tiered-only        Run only the tiered pipeline
#   --fixtures-dir PATH  Directory of seeded fixture JSON files (default: ./fixtures next to this script)
#   --output FILE        Write results to file (default: ./tiered-harness-results.json)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FIXTURES_DIR="${FIXTURES_DIR:-$SCRIPT_DIR/fixtures}"
OUTPUT_FILE="${OUTPUT_FILE:-./tiered-harness-results.json}"
MODE="${MODE:-both}"  # baseline, tiered, or both

while [[ $# -gt 0 ]]; do
  case "$1" in
    --baseline-only) MODE="baseline"; shift ;;
    --tiered-only) MODE="tiered"; shift ;;
    --fixtures-dir) FIXTURES_DIR="$2"; shift 2 ;;
    --output) OUTPUT_FILE="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
command -v bc >/dev/null 2>&1 || { echo "bc is required" >&2; exit 1; }

for f in baseline context decision execute verify; do
  [[ -f "$FIXTURES_DIR/$f.json" ]] || { echo "missing fixture: $FIXTURES_DIR/$f.json" >&2; exit 1; }
done

# Pricing table (offline proxy) — $ per 1k tokens. This is a rate assumption,
# not a measurement; everything downstream of it (tokens, duration) comes
# from the fixture files.
declare -A PRICING=(
  ["small"]="0.001"
  ["mid"]="0.005"
  ["strong"]="0.015"
)

fixture_field() { # $1=step $2=jq filter
  jq -r "$2" "$FIXTURES_DIR/$1.json"
}

step_cost() { # $1=step
  local tier tokens
  tier="$(fixture_field "$1" '.model_tier')"
  tokens=$(( $(fixture_field "$1" '.input_tokens') + $(fixture_field "$1" '.output_tokens') ))
  echo "scale=4; $tokens * ${PRICING[$tier]} / 1000" | bc
}

step_tokens() { # $1=step
  echo $(( $(fixture_field "$1" '.input_tokens') + $(fixture_field "$1" '.output_tokens') ))
}

BASELINE_MODEL="$(fixture_field baseline '.model_tier')"
BASELINE_TOKENS="$(step_tokens baseline)"
BASELINE_COST="$(step_cost baseline)"
BASELINE_WALL_MS="$(fixture_field baseline '.duration_ms')"
BASELINE_PASS="$(fixture_field baseline '.acceptance_pass')"

TIERED_STEPS=(context decision execute verify)
TIERED_TOKENS=0
TIERED_WALL_MS=0
TIERED_COST="0"
for step in "${TIERED_STEPS[@]}"; do
  TIERED_TOKENS=$(( TIERED_TOKENS + $(step_tokens "$step") ))
  TIERED_WALL_MS=$(( TIERED_WALL_MS + $(fixture_field "$step" '.duration_ms') ))
  TIERED_COST=$(echo "scale=4; $TIERED_COST + $(step_cost "$step")" | bc)
done

VERIFY_OUTCOME="$(fixture_field verify '.response.overall')"

COST_SAVED=$(echo "scale=4; $BASELINE_COST - $TIERED_COST" | bc)
COST_REDUCTION=$(echo "scale=2; ($COST_SAVED / $BASELINE_COST) * 100" | bc)
TIME_SAVED_MS=$(( BASELINE_WALL_MS - TIERED_WALL_MS ))
TIME_REDUCTION=$(echo "scale=1; ($TIME_SAVED_MS / $BASELINE_WALL_MS) * 100" | bc)

echo "=== Tiered Execution Cost/Latency Harness (mock/offline — fixtures: $FIXTURES_DIR) ==="
echo ""

if [[ "$MODE" == "baseline" || "$MODE" == "both" ]]; then
  echo "=== Baseline (single $BASELINE_MODEL model) ==="
  echo "Total tokens: $BASELINE_TOKENS"
  echo "Total cost: \$$BASELINE_COST"
  echo "Wall time (mock fixture duration): ${BASELINE_WALL_MS}ms"
  echo "Acceptance: $([[ "$BASELINE_PASS" == "true" ]] && echo PASS || echo FAIL)"
  echo ""
fi

if [[ "$MODE" == "tiered" || "$MODE" == "both" ]]; then
  echo "=== Tiered Execution (context→decision→execute→verify) ==="
  for step in "${TIERED_STEPS[@]}"; do
    tier="$(fixture_field "$step" '.model_tier')"
    printf "%-10s (%-6s): %5d tokens, %6dms, \$%s\n" "$step" "$tier" "$(step_tokens "$step")" "$(fixture_field "$step" '.duration_ms')" "$(step_cost "$step")"
  done
  echo ""
  echo "Total tokens: $TIERED_TOKENS"
  echo "Total cost: \$$TIERED_COST"
  echo "Wall time (mock fixture duration): ${TIERED_WALL_MS}ms"
  echo "Verify outcome: $VERIFY_OUTCOME"
  echo ""
fi

if [[ "$MODE" == "both" ]]; then
  echo "=== Comparison ==="
  echo "Cost savings: \$$COST_SAVED ($COST_REDUCTION%)"
  echo "Time savings: ${TIME_SAVED_MS}ms ($TIME_REDUCTION%)"
  echo ""
fi

echo "NOTE: mock/offline harness — fixture responses are seeded, not from a real"
echo "provider call. These numbers illustrate the tiering shape; they are not"
echo "measured production costs or latencies."
echo ""

if [[ -n "$OUTPUT_FILE" ]]; then
  step_json() { # $1=step
    jq -n \
      --arg model "$(fixture_field "$1" '.model_tier')" \
      --argjson tokens "$(step_tokens "$1")" \
      --arg cost "$(step_cost "$1")" \
      --argjson duration_ms "$(fixture_field "$1" '.duration_ms')" \
      '{model: $model, tokens: $tokens, cost_usd: ($cost|tonumber), duration_ms: $duration_ms}'
  }
  TIERED_STEPS_JSON="{}"
  for step in "${TIERED_STEPS[@]}"; do
    TIERED_STEPS_JSON="$(jq -n --argjson acc "$TIERED_STEPS_JSON" --arg name "$step" --argjson v "$(step_json "$step")" '$acc + {($name): $v}')"
  done

  jq -n \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg fixtures_dir "$FIXTURES_DIR" \
    --arg baseline_model "$BASELINE_MODEL" \
    --argjson baseline_tokens "$BASELINE_TOKENS" \
    --arg baseline_cost "$BASELINE_COST" \
    --argjson baseline_wall_ms "$BASELINE_WALL_MS" \
    --argjson baseline_pass "$BASELINE_PASS" \
    --argjson tiered_steps "$TIERED_STEPS_JSON" \
    --argjson tiered_tokens "$TIERED_TOKENS" \
    --arg tiered_cost "$TIERED_COST" \
    --argjson tiered_wall_ms "$TIERED_WALL_MS" \
    --arg verify_outcome "$VERIFY_OUTCOME" \
    --arg cost_saved "$COST_SAVED" \
    --arg cost_reduction_pct "$COST_REDUCTION" \
    --argjson time_saved_ms "$TIME_SAVED_MS" \
    --arg time_reduction_pct "$TIME_REDUCTION" \
    '{
      timestamp: $timestamp,
      mode: "mock-offline-fixtures",
      note: "Mock/offline harness — every token/cost/time figure is derived from seeded fixture files under fixtures_dir, not a real provider call. Never report as measured production costs.",
      fixtures_dir: $fixtures_dir,
      baseline: {
        model: $baseline_model,
        tokens: $baseline_tokens,
        cost_usd: ($baseline_cost|tonumber),
        wall_time_ms: $baseline_wall_ms,
        acceptance_pass: $baseline_pass
      },
      tiered: {
        steps: $tiered_steps,
        total_tokens: $tiered_tokens,
        total_cost_usd: ($tiered_cost|tonumber),
        wall_time_ms: $tiered_wall_ms,
        verify_outcome: $verify_outcome
      },
      comparison: {
        cost_saved_usd: ($cost_saved|tonumber),
        cost_reduction_percent: ($cost_reduction_pct|tonumber),
        time_saved_ms: $time_saved_ms,
        time_reduction_percent: ($time_reduction_pct|tonumber)
      }
    }' > "$OUTPUT_FILE"
  echo "Results written to: $OUTPUT_FILE"
fi
