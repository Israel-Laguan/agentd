#!/usr/bin/env bash
set -euo pipefail

AGENTD_API_URL="${AGENTD_API_URL:-http://127.0.0.1:8765}"
LITELLM_BASE_URL="${LITELLM_BASE_URL:-http://127.0.0.1:4000/v1}"
LITELLM_API_KEY="${LITELLM_API_KEY:-}"
LITELLM_MODEL="${LITELLM_MODEL:-}"
WEB_URL="${WEB_URL:-http://127.0.0.1:3000}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-300}"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

json_value() {
  python3 -c 'import json,sys
value=json.load(sys.stdin)
for key in sys.argv[1:]:
    value=value.get(key) if isinstance(value, dict) else None
if value is None:
    raise SystemExit(1)
print(value)' "$@"
}

request() {
  curl --fail --silent --show-error --connect-timeout 10 --max-time 30 "$@"
}

need curl
need python3

[ -n "$LITELLM_BASE_URL" ] || fail 'LITELLM_BASE_URL is required'
[ -n "$LITELLM_MODEL" ] || fail 'LITELLM_MODEL is required'

litellm_headers=(-H 'Content-Type: application/json')
if [ -n "$LITELLM_API_KEY" ]; then
  litellm_headers+=(-H "Authorization: Bearer $LITELLM_API_KEY")
fi

models_json=$(request "${litellm_headers[@]}" "$LITELLM_BASE_URL/models") || fail 'LiteLLM models request failed'
printf '%s' "$models_json" | python3 -c 'import json,sys
payload=json.load(sys.stdin)
ids={item.get("id") for item in payload.get("data", []) if isinstance(item, dict)}
if sys.argv[1] not in ids:
    raise SystemExit(1)' "$LITELLM_MODEL" || fail 'LITELLM model is not present in authenticated /v1/models'

request "$AGENTD_API_URL/health" >/dev/null || fail 'agentd health endpoint failed'
request "$AGENTD_API_URL/api/v1/system/status" >/dev/null || fail 'agentd status endpoint failed'
request "$WEB_URL" >/dev/null || fail 'web endpoint failed'

workdir=$(mktemp -d "${TMPDIR:-/tmp}/chat-kanban-qa.XXXXXX")
trap 'rm -rf "$workdir"' EXIT INT TERM
chat_body="$workdir/chat.json"
plan_json="$workdir/plan.json"
materialize_body="$workdir/materialize.json"
materialize_response="$workdir/materialize-response.json"

python3 - "$chat_body" "$LITELLM_MODEL" <<'PY'
import json, sys
path, model = sys.argv[1:]
prompt = '''Create a plan with three tasks. Task 1 must collect execution-environment identity with safe commands such as uname, /etc/os-release, hostname, and id. Task 2 must collect available hardware/resource information with safe commands such as lscpu, free, and lsblk. Task 3 must attempt a privileged hardware-detail lookup, but first seek a safe non-privileged alternative and hand the exact command to a human if no safe alternative exists. Do not claim host hardware; label output as agentd container/execution-environment data.'''
tool = {'type': 'function', 'function': {'name': 'create_plan', 'description': 'Create the requested execution plan.', 'parameters': {'type': 'object', 'properties': {'project_name': {'type': 'string'}, 'description': {'type': 'string'}, 'tasks': {'type': 'array', 'items': {'type': 'object'}}}, 'required': ['project_name', 'tasks']}}}
with open(path, 'w') as stream:
    json.dump({'model': model, 'messages': [{'role': 'user', 'content': prompt}], 'tools': [tool]}, stream)
PY

request "${litellm_headers[@]}" -X POST -H 'Content-Type: application/json' --data-binary "@$chat_body" "$AGENTD_API_URL/v1/chat/completions" > "$workdir/chat-response.json" || fail 'agentd chat request failed'
python3 - "$workdir/chat-response.json" "$plan_json" <<'PY'
import json, sys
with open(sys.argv[1]) as stream:
    response=json.load(stream)
calls=response.get('choices', [{}])[0].get('message', {}).get('tool_calls', [])
for call in calls:
    function=call.get('function', {})
    if function.get('name') == 'create_plan':
        arguments=function.get('arguments', '{}')
        if isinstance(arguments, str):
            arguments=json.loads(arguments)
        with open(sys.argv[2], 'w') as stream:
            json.dump(arguments, stream)
        raise SystemExit(0)
raise SystemExit('create_plan tool call missing')
PY
python3 - "$plan_json" "$materialize_body" <<'PY'
import json, sys
with open(sys.argv[1]) as stream:
    plan=json.load(stream)
with open(sys.argv[2], 'w') as stream:
    json.dump({'project_name': plan['project_name'], 'description': plan.get('description', ''), 'start_empty_workspace': True, 'tasks': plan['tasks']}, stream)
PY

request -X POST -H 'Content-Type: application/json' --data-binary "@$materialize_body" "$AGENTD_API_URL/api/v1/projects/materialize" > "$materialize_response" || fail 'materialization failed'
project_id=$(python3 - "$materialize_response" <<'PY'
import json,sys
with open(sys.argv[1]) as stream:
    response=json.load(stream)
data=response.get('data', response)
print(data['project']['id'])
PY
) || fail 'materialization response has no project id'
printf 'project_id=%s\n' "$project_id"
printf 'materialize_tasks=%s\n' "$materialize_response"

python3 - "$materialize_response" <<'PY' || fail 'root tasks are still PENDING after empty-workspace materialization'
import json,sys
with open(sys.argv[1]) as stream:
    response=json.load(stream)
tasks=response.get('data', response).get('tasks', [])
roots=[task for task in tasks if not task.get('depends_on')]
if not roots or any(task.get('state') == 'PENDING' for task in roots):
    raise SystemExit(1)
PY

completed=0
safe_event=0
attention=''
deadline=$((SECONDS + TIMEOUT_SECONDS))
while [ "$SECONDS" -lt "$deadline" ]; do
  tasks_file="$workdir/tasks.json"
  request "$AGENTD_API_URL/api/v1/projects/$project_id/tasks?limit=200&include_healing=true" > "$tasks_file" || fail 'task polling failed'
  python3 - "$tasks_file" "$workdir/events" <<'PY' || true
import json, os, sys
with open(sys.argv[1]) as stream:
    tasks=json.load(stream).get('data', [])
for task in tasks:
    if task.get('state') == 'COMPLETED' and task.get('title', '').lower().find('identity') >= 0:
        print(task['id'])
PY
  while read -r task_id; do
    [ -n "$task_id" ] || continue
    request "$AGENTD_API_URL/api/v1/tasks/$task_id/events?limit=200" > "$workdir/event.json" || continue
    if python3 - "$workdir/event.json" <<'PY'
import json,sys
with open(sys.argv[1]) as stream:
    events=json.load(stream).get('data', [])
for event in events:
    if event.get('type') == 'RESULT' and any(marker in event.get('payload', '').lower() for marker in ('linux', 'uid=', 'cpu', 'memory')):
        raise SystemExit(0)
raise SystemExit(1)
PY
    then
      completed=1
      safe_event=1
    fi
  done < <(python3 - "$tasks_file" <<'PY'
import json,sys
with open(sys.argv[1]) as stream:
    tasks=json.load(stream).get('data', [])
for task in tasks:
    if task.get('state') == 'COMPLETED' and 'identity' in task.get('title', '').lower():
        print(task['id'])
PY
)
  attention=$(python3 - "$tasks_file" <<'PY'
import json,sys
with open(sys.argv[1]) as stream:
    tasks=json.load(stream).get('data', [])
for task in tasks:
    if task.get('assignee') == 'HUMAN' or task.get('state') in ('BLOCKED','FAILED_REQUIRES_HUMAN'):
        print(task['id'])
        break
PY
)
  if [ "$completed" -eq 1 ] && [ -n "$attention" ]; then
    break
  fi
  sleep 3
done

[ "$completed" -eq 1 ] || fail 'no completed identity task observed'
[ "$safe_event" -eq 1 ] || fail 'no completed RESULT event with environment evidence'
[ -n "$attention" ] || fail 'no human-attention task observed'

printf 'attention_task=%s\n' "$attention"
request -X POST -H 'Content-Type: application/json' --data '{"result":"synthetic QA operator result: no privileged command was executed by agentd"}' "$AGENTD_API_URL/api/v1/tasks/$attention/human-resolution" >/dev/null || fail 'human resolution API failed'
printf 'QA PASS project=%s attention_task=%s\n' "$project_id" "$attention"
