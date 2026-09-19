#!/usr/bin/env bash
# Beat 2.3 helper: disk/resource watchdog fault injection.
# Docs: docs/harness-reliability.md
#
# Proves product-plan Phase 2.3: a disk crunch surfaces a durable
# event or task — no silent death. Uses a high threshold (not a real
# disk fill) so the watchdog fires on any healthy filesystem.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${BIN:-$ROOT/bin/agentd}"
HOME_DIR="${AGENTD_HOME:-/tmp/agentd-disk-demo}"
API_ADDR="${API_ADDR:-127.0.0.1:18785}"
API_URL="http://${API_ADDR}"
# NOTE: WATCHDOG_INTERVAL is configurable but does NOT change daemon polling interval; demo polls API.
WATCHDOG_INTERVAL="${WATCHDOG_INTERVAL:-10m}"

source "$ROOT/scripts/demo/lib/demo-common.sh"

# Derive THRESHOLD deterministically so free_percent < threshold is guaranteed.
derive_threshold() {
  local free_pct
  free_pct=$(python3 -c "import shutil,sys; p=sys.argv[1]; tot,used,free=shutil.disk_usage(p); print(int(free/tot*100))" "$HOME_DIR" 2>/dev/null || echo "50")
  if ! [[ "$free_pct" =~ ^[0-9]+$ ]]; then free_pct=50; fi
  local thr=$((free_pct + 5))
  if (( thr > 99 )); then thr=99; fi
  if (( thr <= free_pct )); then thr=$((free_pct + 1)); fi
  if (( thr > 100 )); then thr=100; fi
  echo "$thr"
}

if [[ -n "${DISK_THRESHOLD:-}" ]]; then
  THRESHOLD="$DISK_THRESHOLD"
elif [[ -d "$HOME_DIR" ]]; then
  # Lazy derive: if HOME_DIR exists use observed free, else 100 guarantees trigger.
  THRESHOLD="$(derive_threshold)"
else
  THRESHOLD="100"
fi

# rewrite_threshold_config keeps config.yaml aligned with the derived threshold.
rewrite_threshold_config() {
  python3 -c "
import sys, pathlib, re
p=sys.argv[1]; thr=sys.argv[2]
t=pathlib.Path(p).read_text()
if re.search(r'free_threshold_percent:\s*[0-9.]+', t):
    t=re.sub(r'free_threshold_percent:\s*[0-9.]+', f'free_threshold_percent: {thr}', t)
else:
    t=t.replace('healing:\n  enabled: false', f'disk:\n  free_threshold_percent: {thr}\nhealing:\n  enabled: false')
pathlib.Path(p).write_text(t)
" "$HOME_DIR/config.yaml" "$THRESHOLD" 2>/dev/null || {
    echo "Failed to rewrite config.yaml with disk threshold" >&2
    return 1
  }
}

usage() {
  cat <<USAGE
Usage: AGENTD_HOME=... $0 <prepare|inject-fault|probe|status|stop>

  prepare      init home + high-threshold config + start daemon
  inject-fault no-op (threshold already triggers on any disk)
  probe        check for _system HUMAN task "Disk space critical..."
  status       print system status + all project tasks
  stop         stop the daemon

Env:
  AGENTD_HOME       (default $HOME_DIR)
  API_ADDR          (default $API_ADDR)
  DISK_THRESHOLD    free % threshold (default $THRESHOLD — 99 ensures trigger)
  WATCHDOG_INTERVAL watchdog check interval (default $WATCHDOG_INTERVAL)
  BIN               (default $BIN)
USAGE
}

cmd_prepare() {
  ensure_bin
  mkdir -p "$HOME_DIR"
  # Documented scratch-directory operation for fault injection.
  mkdir -p "$HOME_DIR/scratch"
  touch "$HOME_DIR/scratch/.disk-watchdog-marker"
  # Derive threshold deterministically when DISK_THRESHOLD not explicitly set,
  # so free_percent < threshold is guaranteed on any healthy filesystem.
  if [[ -z "${DISK_THRESHOLD:-}" ]]; then
    THRESHOLD="$(derive_threshold)"
    echo "Derived disk threshold ${THRESHOLD}% from observed free space (guaranteed trigger)"
  fi
  if [[ ! -f "$HOME_DIR/config.yaml" ]]; then
    cat > "$HOME_DIR/config.yaml" <<YAML
api:
  address: "${API_ADDR}"
gateway:
  order: [openai]
  openai:
    base_url: "http://127.0.0.1:1"
    model: "gpt-4o-mini"
    api_key: "dummy"
disk:
  free_threshold_percent: ${THRESHOLD}
queue:
  task_deadline: "10m"
heartbeat:
  stale_after: "2m"
healing:
  enabled: false
YAML
  else
    # Ensure existing config uses the derived threshold.
    rewrite_threshold_config || return 1
  fi
  # Watchdog interval stays at the daemon default (see WATCHDOG_INTERVAL note);
  # the demo polls the API rather than waiting on a cron trigger.
  start_daemon
  echo ""
  echo "=== Prepare complete ==="
  echo "Disk threshold set to ${THRESHOLD}% — watchdog will fire on next check."
  echo "Run: $0 probe   (after ${WATCHDOG_INTERVAL} or manual wait)"
}

cmd_inject_fault() {
  mkdir -p "$HOME_DIR/scratch"
  touch "$HOME_DIR/scratch/.disk-watchdog-marker"
  if [[ -z "${DISK_THRESHOLD:-}" ]]; then
    THRESHOLD="$(derive_threshold)"
  fi
  # Update config.yaml with the derived threshold so the daemon uses it.
  rewrite_threshold_config || return 1
  echo "Fault injection: scratch dir $HOME_DIR/scratch created; threshold ${THRESHOLD}% derived from observed free space guarantees free_percent < threshold."
  echo "Config updated to free_threshold_percent: ${THRESHOLD} (no real disk fill)."
}

cmd_probe() {
  _probe_cleanup_tmp=()
  trap 'rm -f "${_probe_cleanup_tmp[@]}"' EXIT
  validate_inputs || return 1
  echo "Checking for _system HUMAN task..."
  local resp
  resp=$(curl -fsS -m 5 "$API_URL/api/v1/projects?include_system=true" 2>/dev/null) || {
    echo "API not reachable at $API_URL — is the daemon running?" >&2
    return 1
  }
  local system_pid
  local _sys_tmp
  _sys_tmp=$(mktemp "${TMPDIR:-/tmp}/agentd-sys.XXXXXX") || return 1
  _probe_cleanup_tmp+=("$_sys_tmp")
  echo "$resp" > "$_sys_tmp"
  system_pid=$(python3 -c "
import sys, json
raw = json.load(open(sys.argv[1]))
for p in raw.get('data', raw):
    if p.get('name') == '_system':
        print(p['id'])
        break
" "$_sys_tmp" 2>/dev/null) || true
  if [[ -z "$system_pid" ]]; then
    echo "No _system project yet — watchdog may not have fired."
    echo "Wait a few seconds and try again, or check: $0 status"
    return 1
  fi
  local tasks_resp
  tasks_resp=$(curl -fsS -m 5 "$API_URL/api/v1/projects/$system_pid/tasks" 2>/dev/null) || {
    echo "Failed to fetch tasks for _system project" >&2
    return 1
  }
  local probe_result=""
  local probe_status=0
  local _probe_tmp
  _probe_tmp=$(mktemp "${TMPDIR:-/tmp}/agentd-probe.XXXXXX") || return 1
  _probe_cleanup_tmp+=("$_probe_tmp")
  echo "$tasks_resp" > "$_probe_tmp"
  probe_result=$(python3 -c "
import sys, json
raw = json.load(open(sys.argv[1]))
tasks = raw.get('data', raw)
matches = [t for t in tasks if 'Disk space critical' in t.get('title','')]
human = [t for t in matches if t.get('assignee')=='HUMAN']
if len(matches)==0:
    print('FAIL: no Disk space critical task found'); sys.exit(1)
if len(human)==0:
    print(f'FAIL: Disk space critical task assignee not HUMAN (found {matches[0].get(\"assignee\")})'); sys.exit(2)
if len(matches)!=1:
    print(f'FAIL: expected exactly 1 Disk space critical task, found {len(matches)} (dedup broken)'); sys.exit(3)
t=human[0]
print(f\"PASS: {t.get('state')} task '{t.get('title')}' (assignee=HUMAN) count={len(matches)}\")
" "$_probe_tmp" 2>/dev/null) || probe_status=$?
  if (( probe_status != 0 )); then
    echo "$probe_result"
    echo "The watchdog runs on an interval. Wait and retry, or check: $0 status"
    return 1
  fi
  echo "$probe_result"

  # Verify DISK_SPACE_CRITICAL observed in SSE stream (or daemon.log fallback).
  echo "Checking SSE stream for DISK_SPACE_CRITICAL..."
  local sse_hit=""
  if curl -fsS -m 3 -H "Accept: text/event-stream" "$API_URL/api/v1/events/stream" 2>/dev/null | grep -q "DISK_SPACE_CRITICAL"; then
    sse_hit="sse"
    echo "  SSE: DISK_SPACE_CRITICAL observed"
  elif grep -q "DISK_SPACE_CRITICAL" "$HOME_DIR/daemon.log" 2>/dev/null; then
    sse_hit="log"
    echo "  Log: DISK_SPACE_CRITICAL observed in daemon.log (SSE not reachable but event persisted)"
  else
    # SSE window missed: re-check daemon.log after the task-fetch delay.
    local events_resp
    local _evt_tmp
    _evt_tmp=$(mktemp "${TMPDIR:-/tmp}/agentd-evt.XXXXXX") || return 1
    _probe_cleanup_tmp+=("$_evt_tmp")
    curl -fsS -m 5 "$API_URL/api/v1/projects/$system_pid/tasks" 2>/dev/null > "$_evt_tmp" || true
    events_resp=$(python3 -c "
import sys, json
raw = json.load(open(sys.argv[1]))
for t in raw.get('data', raw):
    if 'Disk space critical' in t.get('title',''):
        print(t.get('id',''))
        break
" "$_evt_tmp" 2>/dev/null || true)
    if [[ -n "$events_resp" ]] && grep -q "DISK_SPACE_CRITICAL" "$HOME_DIR/daemon.log" 2>/dev/null; then
      sse_hit="log"
      echo "  SSE not observed in 3s window — DISK_SPACE_CRITICAL observed in daemon.log"
    else
      echo "FAIL: DISK_SPACE_CRITICAL not observed in SSE stream nor daemon.log" >&2
      return 1
    fi
  fi

  # Verify deduplication: re-fetch tasks after short wait, still exactly 1.
  sleep 1
  local tasks_resp2
  tasks_resp2=$(curl -fsS -m 5 "$API_URL/api/v1/projects/$system_pid/tasks" 2>/dev/null) || {
    echo "Failed to re-fetch tasks for dedup check" >&2
    return 1
  }
  local dedup_count
  local _dedup_tmp
  _dedup_tmp=$(mktemp "${TMPDIR:-/tmp}/agentd-dedup.XXXXXX") || return 1
  _probe_cleanup_tmp+=("$_dedup_tmp")
  echo "$tasks_resp2" > "$_dedup_tmp"
  dedup_count=$(python3 -c "
import sys, json
raw = json.load(open(sys.argv[1]))
tasks = raw.get('data', raw)
print(sum(1 for t in tasks if 'Disk space critical' in t.get('title','')))
" "$_dedup_tmp" 2>/dev/null) || dedup_count="?"
  if [[ "$dedup_count" != "1" ]]; then
    echo "FAIL: deduplication broken — expected 1 Disk space critical task, found $dedup_count after re-poll" >&2
    return 1
  fi
  echo "  Dedup: still 1 task after re-poll (no duplicates)"

  echo ""
  echo "=== Beat 2.3 pass criteria met ==="
  echo "  - _system HUMAN task created (assignee=HUMAN, exactly 1)"
  echo "  - DISK_SPACE_CRITICAL event emitted (via $sse_hit)"
  echo "  - Deduplication verified (no duplicate tasks)"
  echo "  - No silent death — watchdog surfaced the condition"
}

cmd_status() {
  validate_inputs || return 1
  echo "--- System status ---"
  curl -fsS -m 5 "$API_URL/api/v1/system/status" 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "(status unavailable)"
  echo ""
  echo "--- All projects ---"
  curl -fsS -m 5 "$API_URL/api/v1/projects?include_system=true" 2>/dev/null | python3 -c "
import sys, json
raw = json.load(sys.stdin)
projects = raw.get('data', raw)
for p in projects:
    print(f\"  {p['id']}  {p['name']}  ({p.get('status', '?')})\")
" 2>/dev/null || echo "(projects unavailable)"
  echo ""
  local pid
  pid="$(pid_for_home)"
  if [[ -n "$pid" ]]; then
    echo "Daemon running: pid=$pid"
  else
    echo "Daemon not running"
  fi
}

cmd_stop() {
  local pid
  pid="$(pid_for_home)"
  if [[ -z "$pid" ]]; then
    echo "No daemon running for --home $HOME_DIR"
    return 0
  fi
  stop_daemon
  echo "stopped pid=$pid"
}

case "${1:-}" in
  prepare)      cmd_prepare ;;
  inject-fault) cmd_inject_fault ;;
  probe)        cmd_probe ;;
  status)       cmd_status ;;
  stop)         cmd_stop ;;
  *)            usage; exit 1 ;;
esac