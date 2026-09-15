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
PYTHON="${ROOT}/scripts/demo/mock-provider.py"

validate_inputs() {
  if ! [[ "$MOCK_PORT" =~ ^[0-9]+$ ]] || (( MOCK_PORT < 1024 || MOCK_PORT > 65535 )); then
    echo "invalid MOCK_PORT=$MOCK_PORT (want 1024-65535)" >&2
    return 1
  fi
  if [[ "$BIN" =~ [\;\|\&\`\$\(\)\{\}\<\>\"\\] ]]; then
    echo "invalid BIN contains shell metacharacters" >&2
    return 1
  fi
  if [[ "$HOME_DIR" != /* ]]; then
    echo "HOME_DIR must be absolute: $HOME_DIR" >&2
    return 1
  fi
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
  [[ -x "$BIN" ]] || make -C "$ROOT" build
}

pid_for_home() {
  local esc_bin esc_home
  esc_bin=$(printf '%s' "$BIN" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
  esc_home=$(printf '%s' "$HOME_DIR" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
  pgrep -f "^${esc_bin} --home ${esc_home} start([[:space:]]|$)" || true
}

stop_mock() {
  [[ -f "$MOCK_PID_FILE" ]] || return 0
  local pid
  pid="$(cat "$MOCK_PID_FILE" 2>/dev/null || true)"
  if [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o args= 2>/dev/null | grep -Fq -- "mock-provider.py"; then
    kill "$pid" 2>/dev/null || true
    sleep 0.3
    ps -p "$pid" >/dev/null 2>&1 && kill -KILL "$pid" 2>/dev/null || true
  elif [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" >/dev/null 2>&1; then
    echo "stop_mock: PID $pid does not appear to be mock, skipping kill" >&2
  fi
  rm -f "$MOCK_PID_FILE"
}

stop_daemon() {
  local pids pid
  pids="$(pid_for_home)"
  [[ -z "$pids" ]] && return 0
  for pid in $pids; do
    [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o args= 2>/dev/null | grep -qF -- "--home $HOME_DIR" && kill -TERM "$pid" 2>/dev/null || true
  done
  sleep 0.5
  pids="$(pid_for_home)"
  for pid in $pids; do
    [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o args= 2>/dev/null | grep -qF -- "--home $HOME_DIR" && kill -KILL "$pid" 2>/dev/null || true
  done
}

start_mock() {
  validate_inputs || return 1
  mkdir -p "$HOME_DIR"
  stop_mock
  MOCK_PORT="$MOCK_PORT" MOCK_LOG="$HOME_DIR/mock.log" python3 "$PYTHON" server &
  echo $! > "$MOCK_PID_FILE"
  sleep 0.4
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
  [[ -f "$HOME_DIR/global.db" ]] || "$BIN" --home "$HOME_DIR" init
  nohup "$BIN" --home "$HOME_DIR" start --skip-llm-warmup > "$HOME_DIR/daemon.log" 2>&1 &
  local pid=""
  local i
  for i in 1 2 3 4 5 6; do
    pid="$(pid_for_home)"
    [[ -n "$pid" ]] && break
    sleep 0.5
  done
  if [[ -z "$pid" ]]; then
    echo "daemon failed to start (no PID for --home $HOME_DIR after 3s)" >&2
    cat "$HOME_DIR/daemon.log" >&2 || true
    return 1
  fi
  for i in 1 2 3 4 5; do
    if curl -fsS -m 2 "$API_URL/api/v1/system/status" >/dev/null 2>&1; then
      local cur_pid
      cur_pid="$(pid_for_home)"
      if [[ "$cur_pid" != "$pid" ]]; then
        echo "daemon PID changed after readiness (was $pid, now $cur_pid)" >&2
        return 1
      fi
      echo "started pid=$pid api=$API_URL"
      return 0
    fi
    [[ -z "$(pid_for_home)" ]] && { echo "daemon exited during startup (PID $pid gone)" >&2; cat "$HOME_DIR/daemon.log" >&2 || true; return 1; }
    sleep 0.5
  done
  echo "daemon up but API not responding" >&2
  cat "$HOME_DIR/daemon.log" >&2 || true
  return 1
}

cmd_prepare_cascade() {
  stop_daemon; stop_mock; rm -rf "$HOME_DIR"
  start_mock; write_cascade_config; start_daemon
}

cmd_prepare_breaker() {
  stop_daemon; stop_mock; rm -rf "$HOME_DIR"
  write_breaker_config; start_daemon
}

cmd_probe_cascade() {
  echo "=== gateway cascade via $API_URL/v1/chat/completions (primary dead, secondary live) ==="
  local resp
  resp="$(curl -fsS -m 5 -X POST "$API_URL/v1/chat/completions" \
    -H 'Content-Type: application/json' \
    -d '{"model":"mock-secondary","messages":[{"role":"user","content":"hi"}]}' 2>&1)" || {
    echo "gateway probe failed: $resp" >&2; return 1
  }
  echo "$resp"
  echo "$resp" | grep -Fq "fallback ok" || { echo "probe-cascade: response missing fallback content" >&2; return 1; }
  curl -fsS -m 2 "$API_URL/api/v1/system/status" 2>/dev/null | grep -q "secondary" && echo "telemetry: secondary provider observed" || true
  echo
  echo "Pass criterion A: secondary answers while primary base_url is dead (127.0.0.1:1)."
  echo "Coverage: internal/gateway/features/beat2_provider_fallback.feature"
}

cmd_probe_breaker() {
  echo "=== breaker exhaustion via gateway $API_URL/v1/chat/completions ==="
  local attempt breaker_state="" error_body=""
  for attempt in 1 2 3 4 5; do
    local resp
    if ! resp="$(curl -fsS -m 5 -X POST "$API_URL/v1/chat/completions" \
      -H 'Content-Type: application/json' \
      -d '{"model":"dead","messages":[{"role":"user","content":"breaker probe"}]}' 2>&1)"; then
      error_body="$resp"
      echo "attempt $attempt: gateway returned error (expected for dead primary)"
    else
      echo "attempt $attempt: unexpected success" >&2
    fi
    local status
    status="$(curl -fsS -m 2 "$API_URL/api/v1/system/status" 2>/dev/null || true)"
    if echo "$status" | grep -Fq '"breaker":{"state":"OPEN"'; then
      breaker_state="$status"
      echo "breaker telemetry: $status"
      break
    fi
    sleep 0.5
  done
  if ! curl -sS -m 2 "http://127.0.0.1:1/v1/models" >/dev/null 2>&1; then
    echo "primary dead port unreachable (good)"
  else
    echo "unexpected: dead port answered" >&2; return 1
  fi
  if [[ -z "$breaker_state" ]]; then
    echo "FAIL: breaker never reached OPEN state after $attempt attempts" >&2
    return 1
  fi
  echo "$breaker_state" | grep -q "failure_count" && echo "found failure_count" || echo "no failure_count in status (check daemon version)"
  echo "$breaker_state" | grep -q "last_error" && echo "found last_error" || true
  if [[ -z "$error_body" ]]; then
    echo "FAIL: no response body captured with ErrLLMUnreachable" >&2
    return 1
  fi
  echo "Pass criterion B: ErrLLMUnreachable + breaker OPEN after exhaustion."
  echo "Coverage: circuit_breaker.feature + outage_handoff.feature."
  API_URL="$API_URL" python3 "$PYTHON" projects 2>/dev/null || true
  [[ -f "$HOME_DIR/daemon.log" ]] && { echo "=== recent daemon log ==="; tail -n 20 "$HOME_DIR/daemon.log" || true; }
}

cmd_status() {
  echo "=== system/status ==="
  curl -sS -m 5 "$API_URL/api/v1/system/status" || echo "(api down)"
  echo
  echo "=== projects/tasks ==="
  API_URL="$API_URL" python3 "$PYTHON" projects
}

cmd_stop() {
  stop_daemon; stop_mock; echo "stopped"
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
