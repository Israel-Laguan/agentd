"""Per-model control toggles for the mock LLM.

Split out of `server.py` to stay under the 500-line cap T-028 sets for
devenv/, and grouped because they share one rule: each is keyed by model name, so
a journey that owns a model name can drive its own provider's behaviour without
touching any other journey's.

Three toggles:

- `outage` (503) — the provider is unreachable. Feeds the single *global* breaker.
- `quota` (429) — the provider answers but is out of quota. Only
  `ErrLLMQuotaExceeded` reaches the *per-provider* breakers, so a journey that
  wants to exercise a provider breaker needs this, not an outage. See B-016 for why
  that distinction currently cannot be reached through the gateway cascade.
- `slow_once` — the next request for a model sleeps, then proceeds normally, and
  the entry is consumed by that one request. Lets a journey hold a breaker's probe
  in flight long enough to observe what the siblings do while it runs.
"""

import threading
from math import isfinite

OUTAGE_STATUS = 503
QUOTA_STATUS = 429

_lock = threading.Lock()
_outage_models: set = set()
_quota_models: set = set()
_slow_once: dict = {}


def set_outage(model: str, down: bool) -> None:
    with _lock:
        if down:
            _outage_models.add(model)
        else:
            _outage_models.discard(model)


def in_outage(model: str) -> bool:
    with _lock:
        return model in _outage_models


def set_quota(model: str, on: bool) -> None:
    with _lock:
        if on:
            _quota_models.add(model)
        else:
            _quota_models.discard(model)


def in_quota(model: str) -> bool:
    with _lock:
        return model in _quota_models


def set_slow_once(model: str, seconds: float) -> None:
    with _lock:
        _slow_once[model] = seconds


def take_slow_once(model: str) -> float:
    """Consume and return the model's pending one-shot delay, or 0.0."""
    with _lock:
        return _slow_once.pop(model, 0.0)


def read_body(handler) -> dict | None:
    """Decode a control request body, replying 400 on junk.

    Returns the decoded dict, or None when it has already replied and the caller
    must return.
    """
    import json

    length = int(handler.headers.get("Content-Length", "0"))
    raw = handler.rfile.read(length) if length else b"{}"
    try:
        body = json.loads(raw or b"{}")
    except (json.JSONDecodeError, UnicodeDecodeError):
        handler._send({"error": "invalid json"}, 400)
        return None
    if not isinstance(body, dict):
        handler._send({"error": "want a JSON object"}, 400)
        return None
    return body


def handle_control(handler, body: dict) -> None:
    """Route one decoded control request. Replies with 200 on success."""
    if handler.path.rstrip("/").endswith("/outage"):
        model, down = body.get("model", ""), body.get("down")
        if not isinstance(model, str) or not model or not isinstance(down, bool):
            handler._send({"error": "want {model: string, down: bool}"}, 400)
            return
        set_outage(model, down)
        handler._send({"model": model, "down": down})
        return
    if handler.path.rstrip("/").endswith("/quota"):
        model, on = body.get("model", ""), body.get("on")
        if not isinstance(model, str) or not model or not isinstance(on, bool):
            handler._send({"error": "want {model: string, on: bool}"}, 400)
            return
        set_quota(model, on)
        handler._send({"model": model, "on": on})
        return
    if handler.path.rstrip("/").endswith("/slow_once"):
        model, seconds = body.get("model", ""), body.get("seconds")
        if not isinstance(model, str) or not model:
            handler._send({"error": "want {model: string, seconds: number}"}, 400)
            return
        if isinstance(seconds, bool):
            handler._send({"error": "want {model: string, seconds: number}"}, 400)
            return
        try:
            seconds = float(seconds)
        except (TypeError, ValueError):
            handler._send({"error": "want {model: string, seconds: number}"}, 400)
            return
        # math.isfinite, not just `seconds < 0`: NaN compares false against 0 and
        # would survive that check, and both NaN and inf blow up time.sleep()
        # later, turning the next chat-completions request into a 500.
        if not isfinite(seconds) or seconds < 0:
            handler._send({"error": "seconds must be >= 0"}, 400)
            return
        set_slow_once(model, seconds)
        handler._send({"model": model, "seconds": seconds})
        return
    handler._send({"error": "not found"}, 404)