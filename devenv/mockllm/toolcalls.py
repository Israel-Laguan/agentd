"""Tool-call replies for the mock LLM (T-053).

The default mock answers every worker request with a `{"command": ...}` JSON
body, so no journey reaches the agentic tool path: the hook chain, tool results
in the working zone, the context cut. A journey opts in per task, in-band, with
a `@tool_calls=<token>` tag in the task description. The token is URL-safe
base64 of a JSON list of `{"name": str, "arguments": object}`. Like
`@scenario=`, the tag travels with the request, so parallel journeys never
share state.

When a request that offers tools carries the tag:

- with no tool result in the conversation yet, the mock answers with the
  configured `tool_calls` (all in one assistant turn);
- once any tool result is there, it answers with a plain final message, which
  ends the agentic loop.

Requests without `tools` (intake, planning, summaries) never match.
"""

import base64
import binascii
import json
import re

TOOL_CALLS_TAG_RE = re.compile(r"@tool_calls=([A-Za-z0-9_=-]+)")
FINAL_MESSAGE = "tool-call scenario complete"


def encode_tool_calls(calls: list) -> str:
    """Return the tag token for a list of {"name", "arguments"} calls."""
    raw = json.dumps(calls, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(raw).decode()


def configured_tool_calls(body: dict) -> list:
    """Return the calls a request's tag configures, or [] when it has none.

    A malformed token or entry yields [], so a typo falls back to the default
    reply instead of failing the request.
    """
    if not body.get("tools"):
        return []
    for msg in body.get("messages", []):
        content = msg.get("content", "")
        match = TOOL_CALLS_TAG_RE.search(content) if isinstance(content, str) else None
        if not match:
            continue
        try:
            calls = json.loads(base64.urlsafe_b64decode(match.group(1)))
        except (binascii.Error, ValueError):
            return []
        if not isinstance(calls, list) or not all(
            isinstance(c, dict) and isinstance(c.get("name"), str) and c["name"] for c in calls
        ):
            return []
        return calls
    return []


def has_tool_result(body: dict) -> bool:
    return any(msg.get("role") == "tool" for msg in body.get("messages", []))


def tool_calls_completion(body: dict, calls: list) -> dict:
    """An OpenAI chat completion that asks for `calls`, or ends with a final
    message once a tool result is in the conversation."""
    if has_tool_result(body):
        message = {"role": "assistant", "content": FINAL_MESSAGE}
        finish = "stop"
    else:
        message = {
            "role": "assistant",
            "content": None,
            "tool_calls": [
                {
                    "id": f"call_mock_{index}",
                    "type": "function",
                    "function": {"name": call["name"], "arguments": json.dumps(call.get("arguments", {}))},
                }
                for index, call in enumerate(calls)
            ],
        }
        finish = "tool_calls"
    return {
        "id": "chatcmpl-mock-toolcalls",
        "object": "chat.completion",
        "created": 0,
        "model": body.get("model", "mock/agentd"),
        "choices": [{"index": 0, "message": message, "finish_reason": finish}],
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
    }
