#!/usr/bin/env bash
# Beat 1 helper: unclean-kill agentd and restart with the same --home.
# Docs: docs/harness-reliability.md
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${BIN:-$ROOT/bin/agentd}"
HOME_DIR="${AGENTD_HOME:-/tmp/agentd-restart-demo}"
API_ADDR="${API_ADDR:-127.0.0.1:18765}"
API_URL="http://${API_ADDR}"

usage() {
  cat <<USAGE
Usage: AGENTD_HOME=... $0 <prepare|status|kill|restart|cycle>

  prepare  init home + minimal openai-compatible config (dummy upstream OK) + start
  status   print system status + all project tasks
  kill     SIGKILL the agentd process for this --home
  restart  start again with same home (skip warmup)
  cycle    status → kill → restart → status

Env: AGENTD_HOME (default $HOME_DIR), API_ADDR (default $API_ADDR), BIN
USAGE
}

ensure_bin() {
  if [[ ! -x "$BIN" ]]; then
    make -C "$ROOT" build
  fi
}

pid_for_home() {
  local esc_bin esc_home
  esc_bin=$(printf '%s' "$BIN" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
  esc_home=$(printf '%s' "$HOME_DIR" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
  pgrep -f "^${esc_bin} --home ${esc_home} start([[:space:]]|$)" || true
}

cmd_prepare() {
  ensure_bin
  mkdir -p "$HOME_DIR"
  if [[ ! -f "$HOME_DIR/config.yaml" ]]; then
    cat > "$HOME_DIR/config.yaml" <<YAML
api:
  address: "${API_ADDR}"
gateway:
  order: [openai]
  openai:
    base_url: "http://127.0.0.1:1/v1"
    model: "dummy"
    api_key: "dummy"
healing:
  enabled: true
YAML
    echo 'OPENAI_API_KEY=dummy' > "$HOME_DIR/.env"
  fi
  if [[ ! -f "$HOME_DIR/global.db" ]]; then
    "$BIN" --home "$HOME_DIR" init
  fi
  if [[ -n "$(pid_for_home)" ]]; then
    echo "already running pid=$(pid_for_home)"
    return 0
  fi
  nohup "$BIN" --home "$HOME_DIR" start --skip-llm-warmup > "$HOME_DIR/daemon.log" 2>&1 &
  sleep 1
  local pid
  pid="$(pid_for_home)"
  if [[ -z "$pid" ]]; then
    echo "daemon failed to start (no PID for --home $HOME_DIR)" >&2
    cat "$HOME_DIR/daemon.log" >&2 || true
    return 1
  fi
  # Poll API briefly; daemon may still be binding.
  local i
  for i in 1 2 3 4 5; do
    if curl -sS -m 2 "$API_URL/api/v1/system/status" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
    if [[ -z "$(pid_for_home)" ]]; then
      echo "daemon exited after start (PID $pid gone)" >&2
      cat "$HOME_DIR/daemon.log" >&2 || true
      return 1
    fi
    if [[ "$i" -eq 5 ]]; then
      echo "daemon PID $pid running but API $API_URL not responding" >&2
      cat "$HOME_DIR/daemon.log" >&2 || true
      return 1
    fi
  done
  echo "started pid=$pid api=$API_URL log=$HOME_DIR/daemon.log"
}

cmd_status() {
  echo "=== system/status ==="
  curl -sS -m 5 "$API_URL/api/v1/system/status" || echo "(api down)"
  echo
  echo "=== projects/tasks ==="
  python3 - <<'PY' || true
import json, urllib.request, os
base = os.environ.get("API_URL", "http://127.0.0.1:18765")
try:
    payload = json.load(urllib.request.urlopen(base + "/api/v1/projects", timeout=5))
    projects = payload.get("data") or []
except Exception as e:
    print("projects error:", e)
    raise SystemExit(0)
if not projects:
    print("(no projects)")
for p in projects:
    print(p.get("name"), p.get("id"))
    tasks = json.load(urllib.request.urlopen(
        f"{base}/api/v1/projects/{p['id']}/tasks?include_healing=true", timeout=5))
    for t in tasks.get("data", []):
        print(" ", t.get("state"), t.get("assignee"), t.get("title"))
PY
}

cmd_kill() {
  local pids
  pids="$(pid_for_home)"
  if [[ -z "$pids" ]]; then
    echo "no agentd process for --home $HOME_DIR"
    return 0
  fi
  echo "SIGKILL $pids"
  kill -KILL $pids
  # Wait for PID to be reaped (avoid zombie race before restart).
  local i
  for i in 1 2 3 4 5 6; do
    if [[ -z "$(pid_for_home)" ]]; then
      break
    fi
    sleep 0.5
  done
  echo "remaining: $(pid_for_home || echo none)"
}

cmd_restart() {
  ensure_bin
  if [[ -n "$(pid_for_home)" ]]; then
    echo "still running; refuse restart (kill first)" >&2
    return 1
  fi
  nohup "$BIN" --home "$HOME_DIR" start --skip-llm-warmup > "$HOME_DIR/daemon.log" 2>&1 &
  sleep 1
  local pid
  pid="$(pid_for_home)"
  if [[ -z "$pid" ]]; then
    echo "daemon failed to restart (no PID for --home $HOME_DIR)" >&2
    cat "$HOME_DIR/daemon.log" >&2 || true
    return 1
  fi
  local i
  for i in 1 2 3 4 5; do
    if curl -sS -m 2 "$API_URL/api/v1/system/status" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
    if [[ -z "$(pid_for_home)" ]]; then
      echo "daemon exited after restart (PID $pid gone)" >&2
      cat "$HOME_DIR/daemon.log" >&2 || true
      return 1
    fi
    if [[ "$i" -eq 5 ]]; then
      echo "daemon PID $pid running but API $API_URL not responding after restart" >&2
      cat "$HOME_DIR/daemon.log" >&2 || true
      return 1
    fi
  done
  echo "restarted pid=$pid api=$API_URL"
}

cmd_cycle() {
  export API_URL
  cmd_status
  cmd_kill
  cmd_restart
  cmd_status
}

API_URL="$API_URL"
export API_URL HOME_DIR BIN

case "${1:-}" in
  prepare) cmd_prepare ;;
  status) cmd_status ;;
  kill) cmd_kill ;;
  restart) cmd_restart ;;
  cycle) cmd_cycle ;;
  *) usage; exit 1 ;;
esac
