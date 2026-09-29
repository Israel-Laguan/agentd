"""Request capture for the mock LLM.

The mock serves the same OpenAI-shaped surface the real providers do, so a
journey that needs to assert on what the worker actually sent has no way in
through the agentd API. Every request body is appended here as JSONL and read
back over GET /requests.
"""

import json
import os
import sys

# Set MOCKLLM_CAPTURE="" to disable capture entirely.
CAPTURE_PATH = os.environ.get("MOCKLLM_CAPTURE", "/tmp/mockllm-requests.jsonl")


def record_request(body: dict) -> None:
    """Append the request body to the capture log, for e2e assertions.

    Nothing in the agentd API exposes prompt contents, so this is the only
    way a journey can check what the worker actually sent (J11). Append-only
    JSONL so concurrent requests interleave safely; writes are best-effort so
    a missing capture path can never break a request.
    """
    if not CAPTURE_PATH:
        return
    try:
        with open(CAPTURE_PATH, "a", encoding="utf-8") as fh:
            fh.write(json.dumps(body) + "\n")
    except OSError as exc:  # pragma: no cover - diagnostics only
        sys.stderr.write(f"[mockllm] request capture failed: {exc}\n")


def read_requests() -> list:
    """Read back the capture log, skipping any partially-written line."""
    if not CAPTURE_PATH or not os.path.exists(CAPTURE_PATH):
        return []
    entries = []
    with open(CAPTURE_PATH, "r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                entries.append(json.loads(line))
            except json.JSONDecodeError:
                continue
    return entries
