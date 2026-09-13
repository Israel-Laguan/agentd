#!/usr/bin/env python3
"""mockllm - a tiny OpenAI-compatible backend used to drive agentd end-to-end.

It speaks just enough of the Chat Completions contract for agentd's legacy
worker (JSON mode -> {"command": "..."}) and for its plan-decomposition path
({"too_complex": true, "subtasks": [...]}).

Topology in the docker scenario:

    agentd  ->  litellm proxy (model: mock/agentd)  ->  mockllm:8000

The mock reasons only about the TEXT of the incoming task, so the whole
"agentd creates a plan and executes it" flow can be exercised with no real
LLM keys. Correlation metadata that agentd sends (task_id/agent_id/role,
see M18) is logged so you can confirm it flows through litellm.
"""

import json
import os
import sys
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("PORT", "8000"))

# A task whose title contains this marker asks agentd to *create a plan*:
# the mock replies "too_complex" with subtasks, agentd decomposes the task,
# then executes each subtask (which loops back here and returns a command).
PLAN_MARKER = "AGENT_PLAN"


def extract_task_title(body: dict) -> str:
    for msg in body.get("messages", []):
        if msg.get("role") == "user":
            m = re.search(r"Task:\s*(.+)", msg.get("content", ""))
            if m:
                return m.group(1).strip()
    return ""


def build_command(task_id: str, title: str) -> str:
    # Writes evidence of execution into the project workspace (the sandbox cwd),
    # then cats the file so the command stdout becomes the task result.
    # Strip shell-significant characters so the command is always valid bash.
    safe_title = (
        title.replace("\\", " ")
        .replace('"', " ")
        .replace("'", " ")
        .replace("`", " ")
        .replace("$", " ")
        .replace("\n", " ")
    )
    return (
        f'echo "[agentd] task={task_id} title={safe_title} '
        f'executed via litellm proxy" >> PLAN_RESULTS.log && cat PLAN_RESULTS.log'
    )


def chat_completion(body: dict) -> dict:
    md = body.get("metadata") or {}
    task_id = md.get("task_id") or body.get("user") or "unknown"
    title = extract_task_title(body)

    # Log correlation metadata so litellm/M18 wiring is observable in docker logs.
    if md or body.get("user"):
        sys.stderr.write(
            f"[mockllm] correlation task_id={task_id} "
            f"agent_id={md.get('agent_id')} role={md.get('role')}\n"
        )
        sys.stderr.flush()

    if PLAN_MARKER in title:
        sys.stderr.write(f"[mockllm] decomposing plan for task_id={task_id}\n")
        sys.stderr.flush()
        safe_title = title.replace(PLAN_MARKER, "").strip() or title
        content = json.dumps({
            "too_complex": True,
            "subtasks": [
                {"title": f"{safe_title} :: Step 1 - scaffold",
                 "description": "Create the scaffold for the requested work."},
                {"title": f"{safe_title} :: Step 2 - implement",
                 "description": "Implement the core of the requested work."},
            ],
        })
    else:
        content = json.dumps({"command": build_command(task_id, title)})

    return {
        "id": "chatcmpl-mock",
        "object": "chat.completion",
        "created": 0,
        "model": body.get("model", "mock/agentd"),
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
    }


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
        if self.path.rstrip("/").endswith("/models"):
            self._send({"object": "list", "data": [{"id": "mock/agentd"}]})
            return
        self._send({"object": "list", "data": []})

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    sys.stderr.write(f"[mockllm] listening on :{PORT}\n")
    sys.stderr.flush()
    srv.serve_forever()
