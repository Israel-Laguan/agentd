#!/usr/bin/env bash
# Beat 2 helper: cascade-to-secondary vs dead-only breaker exhaustion.
# Docs: docs/harness-reliability.md
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${BIN:-$ROOT/bin/agentd}"
HOME_DIR="${AGENTD_HOME:-/tmp/agentd-fallback-demo}"
API_ADDR="${API_ADDR:-127.0.0.1:18776}"
API_URL="http://${API_ADDR}"
MOCK_PORT="${MOCK_PORT:-18777}"
MOCK_PID_FILE="$HOME_DIR/mock.pid"

# Validate controllable inputs to reduce injection / path-traversal risk.
validate_inputs() {
  if ! [[ "$MOCK_PORT" =~ ^[0-9]+$ ]] || (( MOCK_PORT < 1024 || MOCK_PORT > 65535 )); then
    echo "invalid MOCK_PORT=$MOCK_PORT (want 1024-65535)" >&2
    return 1
  fi
  if [[ "$BIN" == *";"* || "$BIN" == *"|"* || "$BIN" == *"&"* || "$BIN" == *"\`"* ]]; then
    echo "invalid BIN contains shell metacharacters" >&2
    return 1
  fi
  if [[ "$HOME_DIR" != /* ]]; then
    echo "HOME_DIR must be absolute: $HOME_DIR" >&2
    return 1
  fi
  # MOCK_LOG must stay under HOME_DIR (prevent path traversal).
  local mock_log="$HOME_DIR/mock.log"
  if [[ "$(realpath -m "$mock_log")" != "$(realpath -m "$HOME_DIR")"* ]]; then
    echo "MOCK_LOG escapes HOME_DIR" >&2
    return 1
  fi
}

usage() {
  cat <<USAGE
Usage: AGENTD_HOME=... $0 <prepare-cascade|prepare-breaker|probe-cascade|probe-breaker|status|stop>

  prepare-cascade  dead primary + live mock secondary; start daemon
  prepare-breaker  single dead openai slot; start daemon
  probe-cascade    curl mock health + print gateway-ish hint
  probe-breaker    expect dead primary; remind breaker pass criteria
  status           system status + tasks
  stop             stop daemon + mock

Env: AGENTD_HOME, API_ADDR, MOCK_PORT, BIN
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

stop_mock() {
  if [[ -f "$MOCK_PID_FILE" ]]; then
    local pid
    pid="$(cat "$MOCK_PID_FILE" 2>/dev/null || true)"
    # Validate PID is numeric and process appears to be our mock (python http server).
    if [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o args= 2>/dev/null | grep -q "MOCK_PORT"; then
      kill "$pid" 2>/dev/null || true
      # Give it a moment then SIGKILL if still alive.
      sleep 0.3
      if ps -p "$pid" >/dev/null 2>&1; then
        kill -KILL "$pid" 2>/dev/null || true
      fi
    elif [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" >/dev/null 2>&1; then
      # PID exists but doesn't look like our mock — don't kill unrelated process.
      echo "stop_mock: PID $pid does not appear to be mock, skipping kill" >&2
    fi
    rm -f "$MOCK_PID_FILE"
  fi
}

stop_daemon() {
  local pids pid
  pids="$(pid_for_home)"
  if [[ -n "$pids" ]]; then
    # Verify each PID still matches the expected pattern before signalling.
    for pid in $pids; do
      if [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o args= 2>/dev/null | grep -qF -- "--home $HOME_DIR"; then
        kill -TERM "$pid" 2>/dev/null || true
      fi
    done
    sleep 0.5
    # Re-verify before SIGKILL — only kill PIDs that are still the expected daemon.
    pids="$(pid_for_home)"
    for pid in $pids; do
      if [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o args= 2>/dev/null | grep -qF -- "--home $HOME_DIR"; then
        kill -KILL "$pid" 2>/dev/null || true
      fi
    done
  fi
}

start_mock() {
  validate_inputs || return 1
  mkdir -p "$HOME_DIR"
  stop_mock
  # Minimal openai-compatible mock (no billable dependency)
  # Inputs are validated and passed via environment; heredoc is quoted ('PY') so no shell interpolation.
  MOCK_PORT="$MOCK_PORT" MOCK_LOG="$HOME_DIR/mock.log" python3 - <<'PY' &
import json, os
from http.server import BaseHTTPRequestHandler, HTTPServer
port = int(os.environ["MOCK_PORT"])
log_path = os.environ["MOCK_LOG"]
class H(BaseHTTPRequestHandler):
     def log_message(self, fmt, *args):
         with open(log_path, "a") as f:
             f.write("%s - %s\n" % (self.address_string(), fmt % args))
     def do_GET(self):
         body = b'{"ok":true,"role":"secondary-mock"}'
         self.send_response(200)
         self.send_header("Content-Type", "application/json")
         self.send_header("Content-Length", str(len(body)))
         self.end_headers()
         self.wfile.write(body)
     def do_POST(self):
         n = int(self.headers.get("Content-Length", "0"))
         if n:
             self.rfile.read(n)
         payload = {
             "id": "chatcmpl-mock",
             "object": "chat.completion",
             "choices": [{
                 "index": 0,
                 "message": {"role": "assistant", "content": "fallback ok from secondary"},
                 "finish_reason": "stop",
             }],
             "model": "mock-secondary",
         }
         raw = json.dumps(payload).encode()
         self.send_response(200)
         self.send_header("Content-Type", "application/json")
         self.send_header("Content-Length", str(len(raw)))
         self.end_headers()
         self.wfile.write(raw)
HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
  echo $! > "$MOCK_PID_FILE"
  sleep 0.4
  # Validate mock identity, not just TCP connectivity — another service on MOCK_PORT could answer.
  if ! curl -fsS -m 2 "http://127.0.0.1:${MOCK_PORT}/" 2>/dev/null | grep -Fq '"role":"secondary-mock"'; then
    echo "mock failed to start on :$MOCK_PORT" >&2
    stop_mock
    return 1
  fi
  echo "mock pid=$(cat "$MOCK_PID_FILE") http://127.0.0.1:${MOCK_PORT}/"
}

write_cascade_config() {
  mkdir -p "$HOME_DIR"
  cat > "$HOME_DIR/config.yaml" <<YAML
api:
  address: "${API_ADDR}"
gateway:
  order: [primary, secondary]
  providers:
    - name: primary
      adapter: openai
      base_url: "http://127.0.0.1:1/v1"
      model: "dead-primary"
      api_key: "dummy"
    - name: secondary
      adapter: openai
      base_url: "http://127.0.0.1:${MOCK_PORT}/v1"
      model: "mock-secondary"
      api_key: "dummy"
healing:
  enabled: true
YAML
  echo 'OPENAI_API_KEY=dummy' > "$HOME_DIR/.env"
}

write_breaker_config() {
  mkdir -p "$HOME_DIR"
  cat > "$HOME_DIR/config.yaml" <<YAML
api:
  address: "${API_ADDR}"
gateway:
  order: [openai]
  openai:
    base_url: "http://127.0.0.1:1/v1"
    model: "dead"
    api_key: "dummy"
healing:
  enabled: true
YAML
  echo 'OPENAI_API_KEY=dummy' > "$HOME_DIR/.env"
}

start_daemon() {
  validate_inputs || return 1
  ensure_bin
  stop_daemon
  if [[ ! -f "$HOME_DIR/global.db" ]]; then
    "$BIN" --home "$HOME_DIR" init
  fi
  nohup "$BIN" --home "$HOME_DIR" start --skip-llm-warmup > "$HOME_DIR/daemon.log" 2>&1 &
  # Poll for PID with timeout instead of fixed 1s sleep (handles slow startup).
  local pid=""
  local i
  for i in 1 2 3 4 5 6; do
    pid="$(pid_for_home)"
    if [[ -n "$pid" ]]; then
      break
    fi
    sleep 0.5
  done
  if [[ -z "$pid" ]]; then
    echo "daemon failed to start (no PID for --home $HOME_DIR after 3s)" >&2
    cat "$HOME_DIR/daemon.log" >&2 || true
    return 1
  fi
  for i in 1 2 3 4 5; do
    if curl -fsS -m 2 "$API_URL/api/v1/system/status" >/dev/null 2>&1; then
      # Recheck PID liveness after successful curl — another listener could satisfy readiness.
      local cur_pid
      cur_pid="$(pid_for_home)"
      if [[ "$cur_pid" != "$pid" ]]; then
        echo "daemon PID changed after readiness (was $pid, now $cur_pid)" >&2
        return 1
      fi
      echo "started pid=$pid api=$API_URL"
      return 0
    fi
    # Detect daemon exit during poll window.
    if [[ -z "$(pid_for_home)" ]]; then
      echo "daemon exited during startup (PID $pid gone)" >&2
      cat "$HOME_DIR/daemon.log" >&2 || true
      return 1
    fi
    sleep 0.5
  done
  echo "daemon up but API not responding" >&2
  cat "$HOME_DIR/daemon.log" >&2 || true
  return 1
}

cmd_prepare_cascade() {
  stop_daemon
  stop_mock
  rm -rf "$HOME_DIR"
  start_mock
  write_cascade_config
  start_daemon
}

cmd_prepare_breaker() {
  stop_daemon
  stop_mock
  rm -rf "$HOME_DIR"
  write_breaker_config
  start_daemon
}

cmd_probe_cascade() {
  echo "=== gateway cascade via $API_URL/v1/chat/completions (primary dead, secondary live) ==="
  local resp
  resp="$(curl -fsS -m 5 -X POST "$API_URL/v1/chat/completions" \
    -H 'Content-Type: application/json' \
    -d '{"model":"mock-secondary","messages":[{"role":"user","content":"hi"}]}' 2>&1)" || {
    echo "gateway probe failed: $resp" >&2
    return 1
  }
  echo "$resp"
  # Verify gateway routed to secondary (response contains fallback content) and probe secondary still healthy.
  if ! echo "$resp" | grep -Fq "fallback ok"; then
    echo "probe-cascade: response missing fallback content" >&2
    return 1
  fi
  # Cross-check daemon telemetry if available.
  curl -fsS -m 2 "$API_URL/api/v1/system/status" 2>/dev/null | grep -q "secondary" && echo "telemetry: secondary provider observed" || true
  echo
  echo "Pass criterion A: secondary answers while primary base_url is dead (127.0.0.1:1)."
  echo "Coverage: internal/gateway/features/beat2_provider_fallback.feature"
}

cmd_probe_breaker() {
  echo "=== breaker exhaustion via gateway $API_URL/v1/chat/completions ==="
  # Drive repeated requests through gateway until configured threshold; observe breaker telemetry.
  local attempt=0 max_attempts=5 breaker_state=""
  for attempt in 1 2 3 4 5; do
    if ! curl -fsS -m 5 -X POST "$API_URL/v1/chat/completions" \
      -H 'Content-Type: application/json' \
      -d '{"model":"dead","messages":[{"role":"user","content":"breaker probe"}]}' >/dev/null 2>&1; then
      echo "attempt $attempt: gateway returned error (expected for dead primary)"
    else
      echo "attempt $attempt: unexpected success" >&2
    fi
    # Query daemon system status for breaker state.
    local status
    status="$(curl -fsS -m 2 "$API_URL/api/v1/system/status" 2>/dev/null || true)"
    if echo "$status" | grep -Fq '"state":"OPEN"' || echo "$status" | grep -Fq '"breaker"' ; then
      breaker_state="$status"
      echo "breaker telemetry: $status"
      break
    fi
    sleep 0.5
  done
  # Fallback check: dead port itself is unreachable.
  if ! curl -sS -m 2 "http://127.0.0.1:1/v1/models" >/dev/null 2>&1; then
    echo "primary dead port unreachable (good)"
  else
    echo "unexpected: dead port answered" >&2
    return 1
  fi
  # Inspect status payload for expected fields if present.
  if [[ -n "$breaker_state" ]]; then
    echo "$breaker_state" | grep -q "failure_count" && echo "found failure_count" || echo "no failure_count in status (check daemon version)"
    echo "$breaker_state" | grep -q "last_error" && echo "found last_error" || true
  fi
  echo "Pass criterion B: ErrLLMUnreachable + breaker OPEN after threshold."
  echo "Coverage: circuit_breaker.feature + outage_handoff.feature."
  # Check system task data for outage handoff if daemon has been running long enough.
  API_URL="$API_URL" python3 - <<'PY' 2>/dev/null || true
import json, urllib.request, os
base = os.environ.get("API_URL", "http://127.0.0.1:18776")
try:
    payload = json.load(urllib.request.urlopen(base + "/api/v1/projects", timeout=3))
    for p in payload.get("data") or []:
        if p.get("name") == "_system":
            tasks = json.load(urllib.request.urlopen(f"{base}/api/v1/projects/{p['id']}/tasks", timeout=3))
            for t in tasks.get("data", []):
                if "System Offline" in (t.get("title") or "") or "AI API" in (t.get("title") or ""):
                    print("handoff task:", t.get("title"))
except Exception:
    pass
PY
  if [[ -f "$HOME_DIR/daemon.log" ]]; then
    echo "=== recent daemon log ==="
    tail -n 20 "$HOME_DIR/daemon.log" || true
  fi
}

cmd_status() {
  echo "=== system/status ==="
  curl -sS -m 5 "$API_URL/api/v1/system/status" || echo "(api down)"
  echo
  echo "=== projects/tasks ==="
  API_URL="$API_URL" python3 - <<'PY'
import json, urllib.request, os
base = os.environ.get("API_URL", "http://127.0.0.1:18776")
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

cmd_stop() {
  stop_daemon
  stop_mock
  echo "stopped"
}

case "${1:-}" in
  prepare-cascade) cmd_prepare_cascade ;;
  prepare-breaker) cmd_prepare_breaker ;;
  probe-cascade) cmd_probe_cascade ;;
  probe-breaker) cmd_probe_breaker ;;
  status) cmd_status ;;
  stop) cmd_stop ;;
  *) usage; exit 1 ;;
esac

