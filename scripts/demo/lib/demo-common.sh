#!/usr/bin/env bash
# Shared helpers for the scripts/demo/*.sh reliability beat demos.
# Sourced, never executed: callers must set ROOT, BIN, HOME_DIR, API_ADDR and
# API_URL before sourcing this file.
# Docs: docs/harness-reliability.md
# shellcheck shell=bash

if [[ -z "${ROOT:-}" || -z "${BIN:-}" || -z "${HOME_DIR:-}" || -z "${API_ADDR:-}" || -z "${API_URL:-}" ]]; then
  echo "demo-common.sh: set ROOT, BIN, HOME_DIR, API_ADDR and API_URL before sourcing" >&2
  return 1
fi

# ensure_bin builds ./bin/agentd when BIN is missing or not executable.
# A custom BIN path is never silently replaced by the default build output.
ensure_bin() {
  if [[ ! -x "$BIN" ]]; then
    make -C "$ROOT" build || return 1
  fi
  if [[ ! -x "$BIN" ]]; then
    echo "agentd binary is not executable: $BIN (build with 'make -C $ROOT build' or set BIN)" >&2
    return 1
  fi
}

# validate_bin_home rejects shell metacharacters in BIN and relative HOME_DIR.
validate_bin_home() {
  if [[ "$BIN" =~ [\;\|\&\`\$\(\)\{\}\<\>\"\\] ]]; then
    echo "invalid BIN contains shell metacharacters" >&2
    return 1
  fi
  if [[ "$HOME_DIR" != /* ]]; then
    echo "HOME_DIR must be absolute: $HOME_DIR" >&2
    return 1
  fi
}

# validate_api_addr requires host:port or [ipv6]:port with a port of 1-65535.
# Exactly one colon is allowed in the plain form so a bare (unbracketed) IPv6
# literal cannot be mis-split; use the bracketed form for IPv6.
validate_api_addr() {
  local port
  if [[ "$API_ADDR" =~ ^\[[^]]+\]:([0-9]+)$ ]]; then
    port="${BASH_REMATCH[1]}"
  elif [[ "$API_ADDR" =~ ^[^:]+:([0-9]+)$ ]]; then
    port="${BASH_REMATCH[1]}"
  else
    echo "invalid API_ADDR=$API_ADDR (want host:port or [ipv6]:port)" >&2
    return 1
  fi
  if (( 10#$port < 1 || 10#$port > 65535 )); then
    echo "invalid API_ADDR=$API_ADDR (port must be 1-65535)" >&2
    return 1
  fi
}

# validate_inputs is the shared BIN + HOME_DIR + API_ADDR gate.
validate_inputs() {
  validate_bin_home || return 1
  validate_api_addr
}

# pid_for_home lists PIDs of `agentd --home <HOME_DIR> start` processes.
pid_for_home() {
  local esc_bin esc_home
  esc_bin=$(printf '%s' "$BIN" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
  esc_home=$(printf '%s' "$HOME_DIR" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
  pgrep -f "^${esc_bin} --home ${esc_home} start([[:space:]]|$)" || true
}

# pid_list_has reports whether PID $1 appears in the newline-separated list $2.
pid_list_has() {
  local needle="$1" list="$2"
  [[ -n "$needle" && -n "$list" ]] && printf '%s\n' "$list" | grep -qx -- "$needle"
}

# start_daemon boots the daemon for --home, then waits for PID + API readiness.
# Args: $1 optional action label ("start" default, or "restart") for messages.
# Any daemon already bound to the same --home is stopped first, so the PID that
# is verified is always the process launched here.
start_daemon() {
  local action="${1:-start}"
  local label="started"
  if [[ "$action" == "restart" ]]; then label="restarted"; fi
  validate_inputs || return 1
  stop_daemon
  nohup "$BIN" --home "$HOME_DIR" start --skip-llm-warmup > "$HOME_DIR/daemon.log" 2>&1 &
  local launched_pid=$!
  local pid="" i
  # Poll for the PID we launched with timeout (handles slow startup).
  for i in 1 2 3 4 5 6; do
    if pid_list_has "$launched_pid" "$(pid_for_home)"; then
      pid="$launched_pid"
      break
    fi
    sleep 0.5
  done
  if [[ -z "$pid" ]]; then
    echo "daemon failed to $action (launched PID $launched_pid not running for --home $HOME_DIR after 3s)" >&2
    kill -TERM "$launched_pid" 2>/dev/null || true
    cat "$HOME_DIR/daemon.log" >&2 || true
    return 1
  fi
  # Poll API briefly; the daemon may still be binding.
  for i in 1 2 3 4 5; do
    if curl -fsS -m 2 "$API_URL/api/v1/system/status" >/dev/null 2>&1 && pid_list_has "$pid" "$(pid_for_home)"; then
      echo "$label pid=$pid api=$API_URL log=$HOME_DIR/daemon.log"
      return 0
    fi
    sleep 0.5
    if ! pid_list_has "$pid" "$(pid_for_home)"; then
      echo "daemon exited during $action (PID $pid gone)" >&2
      cat "$HOME_DIR/daemon.log" >&2 || true
      return 1
    fi
  done
  echo "daemon PID $pid running but API $API_URL not responding after $action" >&2
  # Do not leak the daemon launched here: stop it so a retry starts clean.
  stop_daemon
  cat "$HOME_DIR/daemon.log" >&2 || true
  return 1
}

# stop_daemon SIGTERMs every daemon for --home, then SIGKILLs any straggler.
stop_daemon() {
  local pids pid i
  pids="$(pid_for_home)"
  if [[ -z "$pids" ]]; then
    return 0
  fi
  for pid in $pids; do
    kill -TERM "$pid" 2>/dev/null || true
  done
  for i in 1 2 3 4 5; do
    sleep 0.5
    if [[ -z "$(pid_for_home)" ]]; then
      return 0
    fi
  done
  pids="$(pid_for_home)"
  for pid in $pids; do
    kill -KILL "$pid" 2>/dev/null || true
  done
  return 0
}
