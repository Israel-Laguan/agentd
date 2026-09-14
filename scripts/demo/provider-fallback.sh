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
    kill "$(cat "$MOCK_PID_FILE")" 2>/dev/null || true
    rm -f "$MOCK_PID_FILE"
  fi
}

stop_daemon() {
  local pids
  pids="$(pid_for_home)"
  if [[ -n "$pids" ]]; then
    kill -TERM $pids 2>/dev/null || true
    sleep 0.5
    kill -KILL $pids 2>/dev/null || true
  fi
}

start_mock() {
  mkdir -p "$HOME_DIR"
  stop_mock
  # Minimal openai-compatible mock (no billable dependency)
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
  if ! curl -sS -m 2 "http://127.0.0.1:${MOCK_PORT}/" >/dev/null; then
    echo "mock failed to start on :$MOCK_PORT" >&2
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
  ensure_bin
  stop_daemon
  if [[ ! -f "$HOME_DIR/global.db" ]]; then
    "$BIN" --home "$HOME_DIR" init
  fi
  nohup "$BIN" --home "$HOME_DIR" start --skip-llm-warmup > "$HOME_DIR/daemon.log" 2>&1 &
  sleep 1
  local pid
  pid="$(pid_for_home)"
  if [[ -z "$pid" ]]; then
    echo "daemon failed to start" >&2
    cat "$HOME_DIR/daemon.log" >&2 || true
    return 1
  fi
  local i
  for i in 1 2 3 4 5; do
    if curl -sS -m 2 "$API_URL/api/v1/system/status" >/dev/null 2>&1; then
      echo "started pid=$pid api=$API_URL"
      return 0
    fi
    sleep 0.5
  done
  echo "daemon up but API not responding" >&2
  cat "$HOME_DIR/daemon.log" >&2 || true
  return 1
}

cmd_prepare_cascade() {
  stop_daemon
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
  echo "=== mock secondary ==="
  curl -sS -m 3 "http://127.0.0.1:${MOCK_PORT}/" || { echo "mock down" >&2; return 1; }
  echo
  echo "=== mock chat completions ==="
  curl -sS -m 5 -X POST "http://127.0.0.1:${MOCK_PORT}/v1/chat/completions" \
    -H 'Content-Type: application/json' \
    -d '{"model":"mock-secondary","messages":[{"role":"user","content":"hi"}]}'
  echo
  echo "Pass criterion A: secondary answers while primary base_url is dead (127.0.0.1:1)."
  echo "Coverage: internal/gateway/features/beat2_provider_fallback.feature"
}

cmd_probe_breaker() {
  echo "=== dead primary (expect connection refused) ==="
  if curl -sS -m 2 "http://127.0.0.1:1/v1/models" >/dev/null 2>&1; then
    echo "unexpected: dead port answered" >&2
    return 1
  fi
  echo "primary unreachable (good)"
  echo "Pass criterion B: ErrLLMUnreachable + breaker OPEN after threshold."
  echo "Coverage: circuit_breaker.feature + outage_handoff.feature."
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

