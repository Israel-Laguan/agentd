#!/bin/sh
# End-to-end test: agentd creates a plan and executes it through litellm.
#
# Runs inside the compose "tester" container (alpine). It materializes a plan,
# waits for the daemon's workers to execute the tasks via the litellm proxy,
# then asserts the work actually happened by reading the sandbox-written
# evidence file (PLAN_RESULTS.log) from the shared projects volume.
#
# Exit code 0 = pass, 1 = fail.

set -u

BASE="http://agentd:8765"
PASS=0
FAIL=0

GREEN='\033[32m'; RED='\033[1;31m'; CYAN='\033[36m'; RESET='\033[0m'
log()  { printf "${CYAN}[e2e]${RESET} %s\n" "$*"; }
pass() { PASS=$((PASS+1)); printf "  ${GREEN}PASS${RESET} %s\n" "$1"; }
fail() { FAIL=$((FAIL+1)); printf "  ${RED}FAIL${RESET} %s\n" "$1"; }

# --- ensure curl + jq are available ------------------------------------------
if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  log "installing curl + jq ..."
  apk add -q --no-cache curl jq >/dev/null 2>&1 || {
    echo "could not install curl/jq; aborting" >&2; exit 1; }
fi

# --- wait for the daemon to be healthy --------------------------------------
log "waiting for agentd at ${BASE} ..."
ready=0
for i in $(seq 1 60); do
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 "${BASE}/api/v1/system/status" 2>/dev/null || echo 000)
  if [ "$code" = "200" ]; then ready=1; break; fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  fail "agentd never became healthy"; echo "RESULT: FAIL"; exit 1
fi
pass "agentd is healthy"

# --- materialize a plan ------------------------------------------------------
log "materializing a plan (one direct task + one AGENT_PLAN task) ..."
# Write the plan to a file (busybox ash has no `read -d ''`).
BODY_FILE="$(mktemp)"
cat > "$BODY_FILE" <<JSON
{
  "project_name": "plan-execute-demo",
  "source_path": "/srv/seed",
  "tasks": [
    {"title": "Generate a greeting script", "description": "Write a hello script to the workspace.", "agent_id": "default"},
    {"title": "AGENT_PLAN: Produce a status report", "description": "A small report task the agent should decompose.", "agent_id": "default"}
  ]
}
JSON

RESP=$(curl -sS -X POST "${BASE}/api/v1/projects/materialize" \
  -H "Content-Type: application/json" --data "@${BODY_FILE}")
PID=$(printf '%s' "$RESP" | jq -r '.data.project.id // empty')
if [ -z "$PID" ]; then
  fail "materialize did not return a project id"; echo "$RESP"; echo "RESULT: FAIL"; exit 1
fi
pass "plan materialized (project=${PID})"

EVIDENCE="/home/agentd/projects/${PID}/PLAN_RESULTS.log"

# --- poll until the kanban settles ------------------------------------------
log "waiting for tasks to execute (max 150s) ..."
settled=0
for i in $(seq 1 150); do
  TASKS=$(curl -sS --max-time 5 "${BASE}/api/v1/projects/${PID}/tasks" | jq -c '.data.data // []')
  active=$(printf '%s' "$TASKS" | jq '[.[] | select(.state | test("READY|QUEUED|RUNNING|PENDING"))] | length')
  if [ "${active:-0}" -eq 0 ]; then settled=1; break; fi
  sleep 1
done

TASKS=$(curl -sS --max-time 5 "${BASE}/api/v1/projects/${PID}/tasks" | jq -c '.data.data // []')
TOTAL=$(printf '%s' "$TASKS" | jq 'length')
COMPLETED=$(printf '%s' "$TASKS" | jq '[.[] | select(.state=="COMPLETED")] | length')
BLOCKED=$(printf '%s' "$TASKS" | jq '[.[] | select(.state=="BLOCKED")] | length')
PLAN_CONTAINER=$(printf '%s' "$TASKS" | jq '[.[] | select(.title | test("AGENT_PLAN"))] | length')

if [ "$settled" -ne 1 ]; then
  fail "kanban did not settle; active tasks remain"
else
  pass "kanban settled (${TOTAL} tasks, ${COMPLETED} completed, ${BLOCKED} blocked)"
fi

# --- assertions --------------------------------------------------------------
if [ "${TOTAL}" -ge 3 ]; then
  pass "agent created a plan (${TOTAL} tasks > 2 seeded: decomposition happened)"
else
  fail "no plan decomposition observed (only ${TOTAL} tasks)"
fi

if [ "${PLAN_CONTAINER}" -ge 1 ]; then
  pass "plan container task present (title contains AGENT_PLAN)"
else
  fail "AGENT_PLAN task missing"
fi

if [ "${COMPLETED}" -ge 1 ]; then
  pass "${COMPLETED} task(s) completed"
else
  fail "no tasks completed"
fi

# --- verify execution evidence written by the sandbox through litellm -------
log "reading execution evidence: ${EVIDENCE}"
if [ ! -f "$EVIDENCE" ]; then
  fail "PLAN_RESULTS.log was not written (tasks did not actually execute)"
else
  lines=$(grep -c "executed via litellm proxy" "$EVIDENCE" 2>/dev/null || echo 0)
  if [ "${lines:-0}" -ge 1 ]; then
    pass "execution evidence present (${lines} command(s) ran via litellm)"
  else
    fail "PLAN_RESULTS.log exists but contains no litellm execution lines"
  fi
  log "----- PLAN_RESULTS.log -----"
  sed 's/^/    /' "$EVIDENCE" 2>/dev/null || true
  log "----------------------------"
fi

# --- summary -----------------------------------------------------------------
if [ "$FAIL" -eq 0 ]; then
  printf "\n${GREEN}RESULT: PASS${RESET} (%s passed)\n" "$PASS"
  exit 0
else
  printf "\n${RED}RESULT: FAIL${RESET} (%s passed, %s failed)\n" "$PASS" "$FAIL"
  exit 1
fi
