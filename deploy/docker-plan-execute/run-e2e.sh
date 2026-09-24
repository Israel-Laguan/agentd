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

RESP=$(curl -sS --connect-timeout 3 --max-time 30 -X POST "${BASE}/api/v1/projects/materialize" \
  -H "Content-Type: application/json" --data "@${BODY_FILE}")
PID=$(printf '%s' "$RESP" | jq -r '.data.project.id // empty' 2>/dev/null || echo '')
if [ -z "$PID" ]; then
  fail "materialize did not return a project id"; echo "$RESP"; echo "RESULT: FAIL"; exit 1
fi
pass "plan materialized (project=${PID})"

EVIDENCE="/home/agentd/projects/${PID}/PLAN_RESULTS.log"

# --- poll until the kanban settles ------------------------------------------
log "waiting for tasks to execute (max 150s) ..."
settled=0
for i in $(seq 1 150); do
  TASKS=$(curl -sS --max-time 5 "${BASE}/api/v1/projects/${PID}/tasks" | jq -c '.data // []' || echo '[]')
  active=$(printf '%s' "$TASKS" | jq '[.[] | select(.state | test("READY|QUEUED|RUNNING|PENDING"))] | length')
  if [ "${active:-0}" -eq 0 ] && [ "$(printf '%s' "$TASKS" | jq 'length')" -gt 0 ]; then settled=1; break; fi
  sleep 1
done

TASKS=$(curl -sS --max-time 5 "${BASE}/api/v1/projects/${PID}/tasks" | jq -c '.data // []' || echo '[]')
if ! printf '%s' "$TASKS" | jq . >/dev/null 2>&1; then
  TASKS='[]'
fi
TOTAL=$(printf '%s' "$TASKS" | jq 'length' || echo 0)
COMPLETED=$(printf '%s' "$TASKS" | jq '[.[] | select(.state=="COMPLETED")] | length' || echo 0)
BLOCKED=$(printf '%s' "$TASKS" | jq '[.[] | select(.state=="BLOCKED")] | length' || echo 0)
PLAN_CONTAINER=$(printf '%s' "$TASKS" | jq '[.[] | select(.title | test("AGENT_PLAN"))] | length' || echo 0)
GEN_SUBTASKS=$(printf '%s' "$TASKS" | jq '[.[] | select(.title | test(":: Step"))] | length' || echo 0)
GEN_COMPLETED=$(printf '%s' "$TASKS" | jq '[.[] | select( (.title | test(":: Step")) and .state=="COMPLETED" )] | length' || echo 0)
DIRECT_COMPLETED=$(printf '%s' "$TASKS" | jq '[.[] | select((.title | test("Generate a greeting script")) and .state=="COMPLETED")] | length' || echo 0)
BLOCKED_IS_PLAN=$(printf '%s' "$TASKS" | jq '[.[] | select(.state=="BLOCKED" and (.title | test("AGENT_PLAN")))] | length' || echo 0)
EXPECTED_COMPLETED=$((TOTAL - 1))

if [ "$settled" -ne 1 ]; then
  fail "kanban did not settle; active tasks remain"
else
  pass "kanban settled (${TOTAL} tasks, ${COMPLETED} completed, ${BLOCKED} blocked; only AGENT_PLAN parent blocked)"
fi

# --- assertions --------------------------------------------------------------
if [ "${GEN_SUBTASKS}" -eq 2 ]; then
  pass "generated plan subtasks present (${GEN_SUBTASKS})"
else
  fail "expected exactly 2 generated plan subtasks, got ${GEN_SUBTASKS}"
fi

if [ "${PLAN_CONTAINER}" -ge 1 ]; then
  pass "plan container task present (title contains AGENT_PLAN)"
else
  fail "AGENT_PLAN task missing"
fi

if [ "${BLOCKED}" -eq 1 ] && [ "${BLOCKED_IS_PLAN}" -eq 1 ]; then
  pass "exactly one BLOCKED task and it contains AGENT_PLAN (the only allowed blocked)"
else
  fail "expected exactly 1 BLOCKED which is the AGENT_PLAN parent (got ${BLOCKED} blocked, ${BLOCKED_IS_PLAN} plan-blocked)"
fi

if [ "${GEN_COMPLETED}" -eq "${GEN_SUBTASKS}" ]; then
  pass "all ${GEN_COMPLETED} generated plan subtask(s) completed (GEN_COMPLETED == GEN_SUBTASKS)"
else
  fail "GEN_COMPLETED (${GEN_COMPLETED}) != GEN_SUBTASKS (${GEN_SUBTASKS}); not all generated subtasks completed"
fi

if [ "${DIRECT_COMPLETED}" -eq 1 ]; then
  pass "original direct greeting task is COMPLETED"
else
  fail "original direct greeting task not completed"
fi

# strict: only the plan parent is BLOCKED; everything else must be COMPLETED
if [ "${COMPLETED}" -eq "${EXPECTED_COMPLETED}" ] && [ "${BLOCKED}" -eq 1 ]; then
  pass "strict: only the one plan parent is BLOCKED, all other tasks COMPLETED"
else
  fail "strict failure: expected ${TOTAL} tasks with ${EXPECTED_COMPLETED} COMPLETED and 1 BLOCKED, got ${COMPLETED} COMPLETED + ${BLOCKED} BLOCKED"
fi

# --- verify execution evidence written by the sandbox through litellm -------
log "reading execution evidence: ${EVIDENCE}"
if [ ! -f "$EVIDENCE" ]; then
  fail "PLAN_RESULTS.log was not written (tasks did not actually execute)"
else
  direct_task_id=$(printf '%s' "$TASKS" | jq -r '.[] | select(.title | test("Generate a greeting script")) | .id')
  if [ -n "$direct_task_id" ] && grep -q "task=${direct_task_id} .*executed via litellm" "$EVIDENCE"; then
    pass "execution evidence present for the direct task"
  else
    fail "PLAN_RESULTS.log has no execution evidence for the direct greeting task"
  fi
  step_evidence=0
  for task_id in $(printf '%s' "$TASKS" | jq -r '.[] | select(.title | test(":: Step")) | .id'); do
    if grep -q "task=${task_id} .*executed via litellm" "$EVIDENCE"; then
      step_evidence=$((step_evidence+1))
    fi
  done
  if [ "${step_evidence}" -eq "${GEN_SUBTASKS}" ]; then
    pass "execution evidence present for generated subtask(s) (${step_evidence})"
  else
    fail "PLAN_RESULTS.log exists but no execution evidence for generated plan subtasks (direct greeting cannot satisfy)"
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
