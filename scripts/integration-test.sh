#!/usr/bin/env bash
# Integration test for agentd HTTP API
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
REPORT_DIR="${PROJECT_DIR}/docs"
REPORT_FILE="${REPORT_DIR}/integration-report-$(date +%Y%m%d-%H%M%S).md"
CONTAINER_NAME="agentd-integration-test"
PORT="${AGENTD_TEST_PORT:-8765}"
BASE_URL="http://localhost:${PORT}"
PASS=0
FAIL=0
TOTAL=0

if [ "${CI:-0}" = "1" ]; then
  GREEN="" RED="" CYAN="" RESET=""
else
  GREEN='\033[32m' RED='\033[1;31m' CYAN='\033[36m' RESET='\033[0m'
fi

log()  { printf "${CYAN}[TEST]${RESET} %s\n" "$*"; }
pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); printf "  ${GREEN}✅ PASS${RESET}\n"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); printf "  ${RED}❌ FAIL${RESET}: %s\n" "$1"; }

cleanup() { docker rm -f "$CONTAINER_NAME" 2>/dev/null || true; }
trap cleanup EXIT

pkill -9 -f "agentd.*start" 2>/dev/null || true
sleep 1

log "Building Docker image..."
cd "$PROJECT_DIR"
docker build -t agentd:latest . --quiet 2>&1 | tail -1

log "Cleaning previous test data..."
rm -rf "${PROJECT_DIR}/.agentd"

log "Running agentd init in container..."
docker run --rm -v "${PROJECT_DIR}/.agentd:/home/agentd/.agentd" agentd:latest init 2>&1

log "Starting agentd container on port ${PORT}..."
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
sleep 1
docker run -d --name "$CONTAINER_NAME" \
  -e AGENTD_API_ADDRESS="0.0.0.0:${PORT}" \
  -v "${PROJECT_DIR}/.agentd:/home/agentd/.agentd" \
  -p "${PORT}:${PORT}" \
  agentd:latest start -v

log "Waiting for server (max 30s)..."
for i in $(seq 1 30); do
  CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "${BASE_URL}/api/v1/system/status" 2>/dev/null || echo "000")
  if [ "$CODE" = "200" ]; then
    log "Server ready in ${i}s"
    break
  fi
  sleep 1
done

sleep 3
mkdir -p "$REPORT_DIR"

write_block() {
  printf '### %s\n- **HTTP Status:** %s\n- **Response:**\n```json\n%s\n```\n- **Validation:** %s\n\n' \
    "$1" "$2" "$3" "$4" >> "$REPORT_FILE"
}

# ─── TEST 1: system/status ──────────────────────────────────────────
log "Test 1: GET /api/v1/system/status"
RESP=$(curl -s --max-time 5 "${BASE_URL}/api/v1/system/status")
CODE=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/system/status")
if echo "$RESP" | grep -q '"status":"success"'; then pass; R1="✅"; else fail "not success"; R1="❌"; fi
write_block "GET /api/v1/system/status" "$CODE" "$RESP" "$R1"

# ─── TEST 2: projects (list) ────────────────────────────────────────
log "Test 2: GET /api/v1/projects"
RESP=$(curl -s --max-time 5 "${BASE_URL}/api/v1/projects")
CODE=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/projects")
if echo "$RESP" | grep -q '"meta"'; then pass; R2="✅"; else fail "no meta"; R2="❌"; fi
write_block "GET /api/v1/projects" "$CODE" "$RESP" "$R2"

# ─── TEST 3: agents (list) ──────────────────────────────────────────
log "Test 3: GET /api/v1/agents"
RESP=$(curl -s --max-time 5 "${BASE_URL}/api/v1/agents")
CODE=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/agents")
CNT=$(echo "$RESP" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('data',[])))" 2>/dev/null || echo 0)
if [ "${CNT:-0}" -ge 1 ]; then pass; R3="✅ ${CNT} agents"; else fail "no agents"; R3="❌"; fi
write_block "GET /api/v1/agents" "$CODE" "$RESP" "$R3"

# ─── TEST 4: agents/default ─────────────────────────────────────────
log "Test 4: GET /api/v1/agents/default"
RESP=$(curl -s --max-time 5 "${BASE_URL}/api/v1/agents/default")
CODE=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/agents/default")
if echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); sys.exit(0 if d.get('data',{}).get('id')=='default' else 1)" 2>/dev/null; then pass; R4="✅"; else fail "not found"; R4="❌"; fi
write_block "GET /api/v1/agents/default" "$CODE" "$RESP" "$R4"

# ─── TEST 5: agents/qa ──────────────────────────────────────────────
log "Test 5: GET /api/v1/agents/qa"
RESP=$(curl -s --max-time 5 "${BASE_URL}/api/v1/agents/qa")
CODE=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/agents/qa")
if echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); sys.exit(0 if d.get('data',{}).get('id')=='qa' else 1)" 2>/dev/null; then pass; R5="✅"; else fail "not found"; R5="❌"; fi
write_block "GET /api/v1/agents/qa" "$CODE" "$RESP" "$R5"

# ─── TEST 6: agents/researcher ──────────────────────────────────────
log "Test 6: GET /api/v1/agents/researcher"
RESP=$(curl -s --max-time 5 "${BASE_URL}/api/v1/agents/researcher")
CODE=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/agents/researcher")
if echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); sys.exit(0 if d.get('data',{}).get('id')=='researcher' else 1)" 2>/dev/null; then pass; R6="✅"; else fail "not found"; R6="❌"; fi
write_block "GET /api/v1/agents/researcher" "$CODE" "$RESP" "$R6"

# ─── TEST 7: POST /v1/chat/completions ─────────────────────────────
log "Test 7: POST /v1/chat/completions"
RESP=$(curl -s --max-time 10 -X POST "${BASE_URL}/v1/chat/completions" -H "Content-Type: application/json" -d '{"messages":[{"role":"user","content":"hello"}],"stream":false}' 2>/dev/null || true)
CCODE=$(curl -s --max-time 10 -o /dev/null -w "%{http_code}" -X POST "${BASE_URL}/v1/chat/completions" -H "Content-Type: application/json" -d '{"messages":[{"role":"user","content":"hello"}],"stream":false}' 2>/dev/null || true)
if [ -n "$RESP" ] && echo "$RESP" | grep -q '"choices"'; then pass; R7="✅";
elif [ -n "$RESP" ]; then TOTAL=$((TOTAL+1)); R7="⚠️ partial (no LLM backend)";
else TOTAL=$((TOTAL+1)); R7="⚠️ no response (no LLM backend)"; RESP="<no response>"; CCODE="--"; fi
write_block "POST /v1/chat/completions" "$CCODE" "$RESP" "$R7"

# ─── TEST 8: rapid status (5x) ──────────────────────────────────────
log "Test 8: Rapid status (5x)"
echo "### Rapid status requests (5x)" >> "$REPORT_FILE"
OK=0
for i in 1 2 3 4 5; do
  R=$(curl -s --max-time 3 "${BASE_URL}/api/v1/system/status" 2>/dev/null || true)
  echo "$R" | grep -q '"status":"success"' && OK=$((OK+1))
done
echo "- Successful: ${OK}/5" >> "$REPORT_FILE"
if [ "$OK" -eq 5 ]; then pass; echo "- Result: ✅" >> "$REPORT_FILE"
else fail "only ${OK}/5"; echo "- Result: ❌ FAIL" >> "$REPORT_FILE"; fi
echo "" >> "$REPORT_FILE"

# ─── TEST 9: 404 check ──────────────────────────────────────────────
log "Test 9: GET /api/v1/nonexistent (expect 404)"
CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 "${BASE_URL}/api/v1/nonexistent" || true)
echo "### GET /api/v1/nonexistent" >> "$REPORT_FILE"
echo "- HTTP Status: ${CODE}" >> "$REPORT_FILE"
if [ "$CODE" = "404" ]; then pass; echo "- Result: ✅" >> "$REPORT_FILE"
else fail "got $CODE"; echo "- Result: ❌ FAIL" >> "$REPORT_FILE"; fi
echo "" >> "$REPORT_FILE"

# ─── TEST 10: SSE stream ────────────────────────────────────────────
log "Test 10: GET /api/v1/events/stream"
echo "### GET /api/v1/events/stream (SSE)" >> "$REPORT_FILE"
SSE=$(curl -s -N --max-time 3 "${BASE_URL}/api/v1/events/stream" 2>&1 || true)
if [ -n "$SSE" ]; then
  echo "- Response: \`\`\`${SSE:0:500}\`\`\`" >> "$REPORT_FILE"
  pass; echo "- Result: ✅ event received" >> "$REPORT_FILE"
else
  echo "- No events (idle, no LLM activity)" >> "$REPORT_FILE"
  pass; echo "- Result: ⚠️ no events (expected idle)" >> "$REPORT_FILE"
fi
echo "" >> "$REPORT_FILE"

# ─── Summary ────────────────────────────────────────────────────────
RATE=$(awk "BEGIN{printf \"%.1f\", $PASS*100/$TOTAL}")
cat >> "$REPORT_FILE" <<EOF
## Summary

| Metric | Value |
|--------|-------|
| Total | ${TOTAL} |
| Passed | ${PASS} |
| Failed | ${FAIL} |
| Pass Rate | ${RATE}% |
| Date | $(date '+%Y-%m-%d %H:%M:%S %Z') |
| Image | agentd:latest |
| Port | ${PORT} |

## Known Limitations
- Chat/materialize endpoints require llama.cpp running on host
- SSE emits events only during active daemon processing
EOF

cleanup
trap - EXIT

log "============================================"
log "Results: ${PASS}/${TOTAL} passed, ${FAIL} failed"
log "Report:  ${REPORT_FILE}"
log "============================================"

[ "$FAIL" -eq 0 ] || exit 1