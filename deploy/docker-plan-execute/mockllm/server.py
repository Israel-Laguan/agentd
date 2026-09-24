#!/usr/bin/env python3
"""OpenAI-compatible mock used by the Docker plan-execution scenario."""

import json
import os
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("PORT", "8000"))
PLAN_MARKER = "AGENT_PLAN"
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
    safe_title = title.replace(PLAN_MARKER, "").strip() or title
    return {
        "too_complex": True,
        "subtasks": [
            {"title": f"{safe_title} :: Step 1 - scaffold", "description": "Create the scaffold for the requested work."},
            {"title": f"{safe_title} :: Step 2 - implement", "description": "Implement the core of the requested work."},
        ],
    }


def chat_completion(body: dict) -> dict:
    metadata = body.get("metadata") or {}
    task_id = metadata.get("task_id") or body.get("user") or "unknown"
    title = extract_task_title(body)
    user_content = message_content(body, "user")
    system_content = message_content(body, "system")

    if metadata or body.get("user"):
        sys.stderr.write(
            f"[mockllm] correlation task_id={task_id} agent_id={metadata.get('agent_id')} "
            f"role={metadata.get('role')}\n"
        )
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
            self._send(chat_completion(body))
            return
        self._send({"error": "not found"}, 404)

    def do_GET(self):
        data = [{"id": "mock/agentd"}] if self.path.rstrip("/").endswith("/models") else []
        self._send({"object": "list", "data": data})

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    sys.stderr.write(f"[mockllm] listening on :{PORT}\n")
    sys.stderr.flush()
    server.serve_forever()
