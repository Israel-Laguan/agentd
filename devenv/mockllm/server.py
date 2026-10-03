#!/usr/bin/env python3
"""OpenAI-compatible mock LLM for the devenv/compose.yaml stack."""

import json
import os
import re
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from capture import CAPTURE_PATH, read_requests, record_request

PORT = int(os.environ.get("PORT", "8000"))
# Where to append every received request body as JSONL. Read back via
# GET /requests. Set MOCKLLM_CAPTURE="" to disable capture entirely.
PLAN_MARKER = "AGENT_PLAN"
SLOW_MARKER = "SLOW_TASK"
PLAN_KEYWORDS = (
    "build", "create", "implement", "design", "plan", "scrape", "tool", "api",
    "web", "app", "script", "cli", "service", "backend", "frontend", "library",
    "framework", "refactor", "migrate", "rewrite", "add", "feature", "component",
)
STATUS_KEYWORDS = (
    "status", "progress", "update", "how are", "what's running", "what is running",
    "what remains", "how things are going", "summary", "current work", "active",
    "remaining",
)
SCOPE_DELIMITER = r'\b(?:and|or|plus|along with|together with|as well as)\s+'
SCOPE_PREFIX = (
    r'(?:build|create|make|add|implement|design|plan|develop)\s+'
    r'(?:a|an|the)?\s*([A-Z][a-zA-Z0-9\s_-]+)'
)
TASK_SPLIT = r'(?:^|\n|\b(?:and|or)\b\s*|,\s*|\d+[)\s]+\s*|[-–*•]\s*)'
SCOPE_INTRODUCER = r'\b(?:build|create|make|add|implement|design|plan|develop)\b'
SCOPE_SPLIT = SCOPE_DELIMITER + r'(?=' + SCOPE_INTRODUCER + r')'

# --- Scenario selection -----------------------------------------------------
#
# A request can select a named behaviour (error, latency, a tiered verify
# outcome) so a journey can drive a specific path without a real provider.
# Selection is per request and resolved in this order:
#   1. an in-band @scenario=<name> tag in any message (travels with the
#      request, so parallel journeys never share state);
#   2. an X-Mock-Scenario header;
#   3. the request's model name, mapped by MOCKLLM_MODEL_SCENARIOS
#      (e.g. "gpt-3.5-turbo=tiered-fail-verify,gpt-4=tiered-pass-verify").
SCENARIO_TAG_RE = re.compile(r"@scenario=([A-Za-z0-9_-]+)")

# Models currently "down". Unlike a scenario tag, which fails one request, an
# outage fails every request for a model until it is switched off, so a journey
# can end a provider outage mid-test (J09 recovery). Keyed by model name so only
# the profile that owns that model sees it; every other journey is unaffected.
# Switched through POST /outage {"model": "<name>", "down": true|false}.
OUTAGE_STATUS = 503
_outage_lock = threading.Lock()
_outage_models: set = set()


def set_outage(model: str, down: bool) -> None:
    with _outage_lock:
        if down:
            _outage_models.add(model)
        else:
            _outage_models.discard(model)


def in_outage(model: str) -> bool:
    with _outage_lock:
        return model in _outage_models
DEFAULT_LIMIT = 200


def message_content(body: dict, role: str) -> str:
    return next(
        (msg.get("content", "") for msg in body.get("messages", []) if msg.get("role") == role),
        "",
    )


def select_scenario(body: dict, headers) -> str:
    """Return the scenario name selected by this request, or "" for the default.

    `headers` is a email.message.Message (BaseHTTPRequestHandler.headers); header
    lookup is case-insensitive.
    """
    for msg in body.get("messages", []):
        content = msg.get("content", "")
        if isinstance(content, str):
            match = SCENARIO_TAG_RE.search(content)
            if match:
                return match.group(1)
    header = headers.get("X-Mock-Scenario") if headers is not None else None
    if header and header.strip():
        return header.strip()
    model = body.get("model", "")
    mapping = os.environ.get("MOCKLLM_MODEL_SCENARIOS", "")
    for pair in mapping.split(","):
        if "=" not in pair:
            continue
        name, scenario = pair.split("=", 1)
        if name.strip() == model:
            return scenario.strip()
    return ""


def extract_task_title(body: dict) -> str:
    match = re.search(r"Task:\s*(.+)", message_content(body, "user"))
    return match.group(1).strip() if match else ""


def classify_intent(user_content: str) -> dict:
    lower = user_content.lower()
    if any(keyword in lower for keyword in STATUS_KEYWORDS):
        return {"intent": "status_check", "reason": "user asked about status or progress"}
    if any(keyword in lower for keyword in PLAN_KEYWORDS):
        return {"intent": "plan_request", "reason": "user wants to build or plan new software work"}
    return {
        "intent": "ambiguous",
        "reason": "request does not clearly indicate a plannable software task",
    }


def build_command(task_id: str, title: str) -> str:
    safe_title = title.translate(str.maketrans("", "", "\\\"'`$\n"))
    return (
        f'echo "[agentd] task={task_id} title={safe_title} '
        f'executed via litellm proxy" >> PLAN_RESULTS.log && cat PLAN_RESULTS.log'
    )


# Long enough for a journey to kill the daemon mid-task, short enough to keep J08
# fast. Output on either stream resets the sandbox inactivity timer (B-011), so this
# is a time budget, not a limit imposed by the 60s inactivity timeout.
SLOW_TICKS = 30


def slow_command():
    # Emits output every second; lets a journey kill the daemon while the task is
    # RUNNING, and the last line ("tick 29") shows the command ran to the end.
    # It also leaves attempt.marker and reports "carried-over" when one is already
    # there, so a re-run shows whether the workspace was reset between attempts.
    return (
        'if [ -e attempt.marker ]; then echo carried-over; fi; touch attempt.marker; '
        f'i=0; while [ $i -lt {SLOW_TICKS} ]; do echo tick $i; i=$((i+1)); sleep 1; done'
    )


def task_from_text(part: str) -> dict:
    title = part.strip()
    if len(title) > 120:
        title = title[:117] + "..."
    return {"title": title, "description": title, "assignee": "SYSTEM"}


def generate_plan(user_content: str) -> dict:
    project_name = "Untitled Project"
    match = re.search(
        r'(?:create|build|make|add|implement|design|plan)\s+(?:a|an|the)?\s*'
        r'([A-Z][a-zA-Z0-9\s_-]+?)(?:\s+(?:with|for|that|to|in|using|and|or)|(?:\.|$))',
        user_content,
        re.IGNORECASE,
    )
    if match:
        project_name = match.group(1).strip()
        if len(project_name) > 60:
            project_name = project_name[:57] + "..."

    tasks = []
    task_list = re.search(
        r'(?:tasks?|steps?|items?|actions?)\s*[:\-–=]\s*(.+)',
        user_content,
        re.IGNORECASE | re.DOTALL,
    )
    if task_list:
        parts = re.split(
            TASK_SPLIT,
            task_list.group(1).strip(),
        )
        tasks = [task_from_text(part) for part in parts if len(part.strip()) >= 3][:5]
    if not tasks:
        tasks = [
            {
                "title": f"Set up {project_name}",
                "description": f"Initialize the project structure for {project_name}.",
                "assignee": "SYSTEM",
            },
            {
                "title": f"Implement core of {project_name}",
                "description": f"Build the main functionality of {project_name}.",
                "assignee": "SYSTEM",
            },
        ]
    return {
        "project_name": project_name,
        "description": user_content.strip(),
        "tasks": tasks,
    }


def scope_label(part: str, single_scope: bool) -> str:
    match = re.search(SCOPE_PREFIX, part, re.IGNORECASE)
    if match:
        label = match.group(1).strip()
    elif single_scope:
        label = part.strip()[:80]
    else:
        label = part[:60]
    return label if len(label) <= 60 else label[:57] + "..."


def analyze_scope(user_content: str) -> dict:
    multi_scope = re.search(SCOPE_SPLIT, user_content, re.IGNORECASE)
    single_scope = multi_scope is None
    if single_scope:
        scopes = [{"id": "scope-1", "label": scope_label(user_content, True)}]
    else:
        parts = re.split(SCOPE_SPLIT, user_content, flags=re.IGNORECASE)
        scopes = [
            {"id": f"scope-{index}", "label": scope_label(part, False)}
            for index, part in enumerate(parts, 1)
            if part.strip()
        ]
    return {
        "single_scope": single_scope,
        "confidence": 0.85 if single_scope else 0.7,
        "scopes": scopes,
        "reason": "single cohesive deliverable" if single_scope else "multiple distinct deliverables detected",
    }


def completion(body: dict, content: dict, response_id: str = "chatcmpl-mock") -> dict:
    return {
        "id": response_id,
        "object": "chat.completion",
        "created": 0,
        "model": body.get("model", "mock/agentd"),
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": json.dumps(content)},
            "finish_reason": "stop",
        }],
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
    }


def decompose_plan(title: str) -> dict:
    safe_title = title.replace(PLAN_MARKER, "").strip().lstrip(":").strip() or title
    return {
        "too_complex": True,
        "subtasks": [
            {"title": f"{safe_title} :: Step 1 - scaffold", "description": "Create the scaffold for the requested work."},
            {"title": f"{safe_title} :: Step 2 - implement", "description": "Implement the core of the requested work."},
        ],
    }


# --- Tiered execution replies ----------------------------------------------
#
# The tiered worker runs each pipeline step (context/decision/execute/verify/
# escalate) through the agentic engine with a step-specific system-prompt
# suffix. The mock detects the step from that suffix and returns the artifact
# the step commits, so a tiered pipeline runs end to end without a real
# provider. The verify step fails by default, which drives the escalation
# ladder (mid-fix redos, then a strong-model escalate) that J12 observes.
TIERED_STEP_MARKERS = {
    "TIERED MODE: CONTEXT STEP": "context",
    "TIERED MODE: DECISION STEP": "decision",
    "TIERED MODE: EXECUTE STEP": "execute",
    "TIERED MODE: VERIFY STEP": "verify",
    "TIERED MODE: ESCALATE STEP": "escalate",
}


def tiered_step(system_content: str) -> str:
    for marker, step in TIERED_STEP_MARKERS.items():
        if marker in system_content:
            return step
    return ""


def tiered_reply(step: str) -> dict:
    if step == "context":
        # Minimal but structurally valid ContextPack: parseAndConfigurePack
        # overwrites task_id/parent_task_id and the budget counters, and
        # Validate only checks structure (version, non-empty summary, >=1
        # unique path, consistent budget), not that paths exist on disk.
        return {
            "version": 1,
            "summary": "Workspace context gathered for this task.",
            "paths": ["README.md"],
            "constraints": [],
            "unknowns": [],
        }
    if step == "decision":
        return {
            "touch_list": ["README.md"],
            "checks": ["echo verify"],
            "rationale": "Minimal decision for the mock.",
        }
    if step == "verify":
        return {
            "results": [{"check": "echo verify", "outcome": "fail", "detail": "mock verify failure"}],
            "overall": "fail",
        }
    # execute / escalate: a terminal success is all the step needs.
    return {"status": "ok", "output": f"mock {step} success"}


def request_metadata(body: dict) -> dict:
    # litellm keeps the client's `metadata` for itself; its agentd_correlation
    # hook re-sends those fields as `agentd_metadata`. Plain `metadata` still
    # works when agentd talks to this mock directly.
    return body.get("agentd_metadata") or body.get("metadata") or {}


def scenario_error_status(scenario: str) -> int:
    # "error" -> 500, "error-429" -> 429, etc.
    if scenario.startswith("error-"):
        try:
            return int(scenario.split("-", 1)[1])
        except ValueError:
            return 500
    return 500


def chat_completion(body: dict, headers=None) -> dict:
    metadata = request_metadata(body)
    task_id = metadata.get("task_id") or body.get("user") or "unknown"
    title = extract_task_title(body)
    user_content = message_content(body, "user")
    system_content = message_content(body, "system")

    if metadata or body.get("user"):
        sys.stderr.write(
            f"[mockllm] correlation task_id={task_id} agent_id={metadata.get('agent_id')} "
            f"role={metadata.get('role')}\n"
        )
    else:
        sys.stderr.write(f"[mockllm] no correlation; request keys={sorted(body)}\n")
    sys.stderr.flush()

    # A selected scenario overrides the prompt-inferred behaviour.
    scenario = select_scenario(body, headers)
    if scenario and not scenario.startswith(("error", "latency", "slow", "timeout")):
        sys.stderr.write(f"[mockllm] scenario={scenario}\n")
        sys.stderr.flush()

    step = tiered_step(system_content)
    if step:
        sys.stderr.write(f"[mockllm] tiered step={step} task_id={task_id}\n")
        sys.stderr.flush()
        return completion(body, tiered_reply(step), f"chatcmpl-mock-tiered-{step}")

    if "Frontdesk scope analyzer" in system_content:
        scope_analysis = analyze_scope(user_content)
        sys.stderr.write(
            f"[mockllm] scope analysis: single_scope={scope_analysis['single_scope']} "
            f"scopes={len(scope_analysis['scopes'])}\n"
        )
        sys.stderr.flush()
        return completion(body, scope_analysis, "chatcmpl-mock-scope")

    if "Frontdesk intent classifier" in system_content:
        intent = classify_intent(user_content)
        sys.stderr.write(f"[mockllm] intent classification: {intent['intent']}\n")
        sys.stderr.flush()
        return completion(body, intent, "chatcmpl-mock-intent")

    if "Output ONLY valid JSON" in system_content:
        plan = generate_plan(user_content)
        sys.stderr.write(f"[mockllm] generating plan for: {plan['project_name']}\n")
        sys.stderr.flush()
        return completion(body, plan, "chatcmpl-mock-plan")

    if PLAN_MARKER in title:
        sys.stderr.write(f"[mockllm] decomposing plan for task_id={task_id}\n")
        sys.stderr.flush()
        return completion(body, decompose_plan(title))
    if SLOW_MARKER in title:
        return completion(body, {"command": slow_command()})
    return completion(body, {"command": build_command(task_id, title)})


class Handler(BaseHTTPRequestHandler):
    def _send(self, payload: dict, status: int = 200):
        data = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        if self.path.rstrip("/").endswith("/chat/completions"):
            length = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(length) if length else b"{}"
            try:
                body = json.loads(raw or b"{}")
            except json.JSONDecodeError:
                self._send({"error": "invalid json"}, 400)
                return
            if in_outage(body.get("model", "")):
                sys.stderr.write(f"[mockllm] outage model={body.get('model')} -> HTTP {OUTAGE_STATUS}\n")
                sys.stderr.flush()
                self._send({"error": "mock outage"}, OUTAGE_STATUS)
                return
            scenario = select_scenario(body, self.headers)
            if scenario.startswith("error"):
                status = scenario_error_status(scenario)
                sys.stderr.write(f"[mockllm] scenario={scenario} -> HTTP {status}\n")
                sys.stderr.flush()
                self._send({"error": f"mock error scenario {scenario}"}, status)
                return
            if scenario in ("latency", "slow", "timeout"):
                delay = float(os.environ.get("MOCKLLM_LATENCY", "2"))
                sys.stderr.write(f"[mockllm] scenario={scenario} sleeping {delay}s\n")
                sys.stderr.flush()
                time.sleep(delay)
            record_request(body)
            self._send(chat_completion(body, self.headers))
            return
        if self.path.rstrip("/").endswith("/outage"):
            length = int(self.headers.get("Content-Length", "0"))
            try:
                body = json.loads((self.rfile.read(length) if length else b"{}") or b"{}")
            except json.JSONDecodeError:
                self._send({"error": "invalid json"}, 400)
                return
            model = body.get("model", "")
            if not model or not isinstance(body.get("down"), bool):
                self._send({"error": "want {model: string, down: bool}"}, 400)
                return
            set_outage(model, body["down"])
            self._send({"model": model, "down": body["down"]})
            return
        self._send({"error": "not found"}, 404)

    def do_GET(self):
        path = self.path.rstrip("/")
        if path.endswith("/models"):
            data = [{"id": "mock/agentd"}]
        elif path.endswith("/requests"):
            # Read back the capture log so a journey can assert on what the
            # worker actually sent without shelling into the container.
            data = read_requests()
        else:
            data = []
        self._send({"object": "list", "data": data})

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    sys.stderr.write(f"[mockllm] listening on :{PORT}\n")
    sys.stderr.flush()
    server.serve_forever()
