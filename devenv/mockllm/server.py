#!/usr/bin/env python3
"""OpenAI-compatible mock LLM for the devenv/compose.yaml stack."""

import json
import os
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("PORT", "8000"))
# Where to append every received request body as JSONL. Read back via
# GET /requests. Set MOCKLLM_CAPTURE="" to disable capture entirely.
CAPTURE_PATH = os.environ.get("MOCKLLM_CAPTURE", "/tmp/mockllm-requests.jsonl")
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


def message_content(body: dict, role: str) -> str:
    return next(
        (msg.get("content", "") for msg in body.get("messages", []) if msg.get("role") == role),
        "",
    )


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


def slow_command() -> str:
    # Emits output every second so the executor's inactivity timeout never
    # fires; lets a journey kill the daemon while the task is RUNNING.
    return 'i=0; while [ $i -lt 60 ]; do echo tick $i; i=$((i+1)); sleep 1; done'


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


def request_metadata(body: dict) -> dict:
    # litellm keeps the client's `metadata` for itself; its agentd_correlation
    # hook re-sends those fields as `agentd_metadata`. Plain `metadata` still
    # works when agentd talks to this mock directly.
    return body.get("agentd_metadata") or body.get("metadata") or {}


def chat_completion(body: dict) -> dict:
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


def record_request(body: dict) -> None:
    """Append the full request body to the capture log, for e2e assertions.

    J11 needs to prove that a preference the user saved through
    POST /api/v1/preferences actually reached the worker's prompt. Nothing
    in the agentd API exposes prompt contents, and stderr only carries
    correlation metadata, so without a durable capture the journey would have
    to take the preference's presence on faith — the same self-fulfilling
    check that scripts/demo/memory-recall.sh performs.

    The log is append-only JSONL so concurrent worker requests interleave
    safely (the server is threaded) and a test can read the entries written
    during one task's execution. Writes are best-effort: a read-only or
    missing capture directory must never break a request.
    """
    if not CAPTURE_PATH:
        return
    try:
        with open(CAPTURE_PATH, "a", encoding="utf-8") as fh:
            fh.write(json.dumps(body) + "\n")
    except OSError as exc:  # pragma: no cover - diagnostics only
        sys.stderr.write(f"[mockllm] request capture failed: {exc}\n")


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
            record_request(body)
            self._send(chat_completion(body))
            return
        self._send({"error": "not found"}, 404)

    def do_GET(self):
        path = self.path.rstrip("/")
        if path.endswith("/models"):
            data = [{"id": "mock/agentd"}]
        elif path.endswith("/requests"):
            # Read back the capture log so a journey can assert on what the
            # worker actually sent without shelling into the container.
            entries = []
            if CAPTURE_PATH and os.path.exists(CAPTURE_PATH):
                with open(CAPTURE_PATH, "r", encoding="utf-8") as fh:
                    for line in fh:
                        line = line.strip()
                        if line:
                            try:
                                entries.append(json.loads(line))
                            except json.JSONDecodeError:
                                continue
            data = entries
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
