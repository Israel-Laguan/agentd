#!/usr/bin/env python3
"""Unit tests for the mock LLM's scenario dispatch and tiered replies.

Run with:  python3 -m unittest discover -s devenv/mockllm -p 'test_server.py'
(or `make test-mockllm` from the repo root).
"""

import json
import sys
import threading
import unittest
import urllib.error
import urllib.request
from email.message import Message
from http.server import ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import server  # noqa: E402


def headers(**kwargs):
    """Build an email.message.Message the way BaseHTTPRequestHandler.headers looks."""
    msg = Message()
    for key, value in kwargs.items():
        msg[key] = value
    return msg


class SelectScenarioTest(unittest.TestCase):
    def test_default_is_empty(self):
        self.assertEqual(server.select_scenario({"messages": []}, headers()), "")

    def test_in_band_tag_in_user_message(self):
        body = {"messages": [{"role": "user", "content": "build a thing @scenario=error"}]}
        self.assertEqual(server.select_scenario(body, headers()), "error")

    def test_in_band_tag_in_system_message(self):
        body = {"messages": [{"role": "system", "content": "@scenario=latency"}]}
        self.assertEqual(server.select_scenario(body, headers()), "latency")

    def test_tag_wins_over_header(self):
        body = {"messages": [{"role": "user", "content": "@scenario=error"}]}
        self.assertEqual(server.select_scenario(body, headers(**{"X-Mock-Scenario": "latency"})), "error")

    def test_header_selection(self):
        body = {"messages": [{"role": "user", "content": "build a thing"}]}
        self.assertEqual(server.select_scenario(body, headers(**{"X-Mock-Scenario": "slow"})), "slow")

    def test_model_name_selection(self):
        body = {"model": "gpt-3.5-turbo", "messages": []}
        import os

        os.environ["MOCKLLM_MODEL_SCENARIOS"] = "gpt-3.5-turbo=tiered-fail-verify"
        try:
            self.assertEqual(server.select_scenario(body, headers()), "tiered-fail-verify")
        finally:
            del os.environ["MOCKLLM_MODEL_SCENARIOS"]


class TieredStepTest(unittest.TestCase):
    def test_detects_each_step(self):
        for marker, step in server.TIERED_STEP_MARKERS.items():
            self.assertEqual(server.tiered_step(f"prefix {marker} suffix"), step)

    def test_non_tiered_returns_empty(self):
        self.assertEqual(server.tiered_step("Frontdesk scope analyzer"), "")

    def test_tiered_replies_have_expected_shape(self):
        context = server.tiered_reply("context")
        self.assertEqual(context["version"], 1)
        self.assertTrue(context["summary"])
        self.assertGreaterEqual(len(context["paths"]), 1)

        decision = server.tiered_reply("decision")
        self.assertIn("touch_list", decision)
        self.assertIn("checks", decision)

        verify = server.tiered_reply("verify")
        self.assertEqual(verify["overall"], "fail")
        self.assertGreaterEqual(len(verify["results"]), 1)

        self.assertEqual(server.tiered_reply("execute")["status"], "ok")
        self.assertEqual(server.tiered_reply("escalate")["status"], "ok")


class ScenarioErrorStatusTest(unittest.TestCase):
    def test_default_error_is_500(self):
        self.assertEqual(server.scenario_error_status("error"), 500)

    def test_specific_status(self):
        self.assertEqual(server.scenario_error_status("error-429"), 429)
        self.assertEqual(server.scenario_error_status("error-503"), 503)

    def test_invalid_specific_status_falls_back(self):
        self.assertEqual(server.scenario_error_status("error-abc"), 500)


class ChatCompletionDispatchTest(unittest.TestCase):
    def _body(self, system="", user="build a thing"):
        return {"model": "agentd", "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ]}

    def test_tiered_context_returns_context_pack(self):
        body = self._body(system="TIERED MODE: CONTEXT STEP")
        result = server.chat_completion(body, headers())
        content = result["choices"][0]["message"]["content"]
        self.assertIn('"version": 1', content)
        self.assertIn('"paths"', content)

    def test_tiered_verify_returns_failing_result(self):
        body = self._body(system="TIERED MODE: VERIFY STEP")
        result = server.chat_completion(body, headers())
        content = result["choices"][0]["message"]["content"]
        self.assertIn('"overall": "fail"', content)

    def test_tiered_escalate_returns_success(self):
        body = self._body(system="TIERED MODE: ESCALATE STEP")
        result = server.chat_completion(body, headers())
        content = result["choices"][0]["message"]["content"]
        self.assertIn('"status": "ok"', content)

    def test_non_tiered_falls_through_to_command(self):
        body = self._body(system="some other prompt", user="build a thing")
        result = server.chat_completion(body, headers())
        content = result["choices"][0]["message"]["content"]
        self.assertIn("command", content)


class OutageTest(unittest.TestCase):
    """POST /outage fails every request for one model until switched off."""

    @classmethod
    def setUpClass(cls):
        cls.httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        cls.base = f"http://127.0.0.1:{cls.httpd.server_port}"
        threading.Thread(target=cls.httpd.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()
        cls.httpd.server_close()

    def tearDown(self):
        server.set_outage("brk-outage", False)
        server.set_outage("other", False)

    def _post(self, path, payload):
        req = urllib.request.Request(
            self.base + path, data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"}
        )
        try:
            with urllib.request.urlopen(req) as resp:
                return resp.status
        except urllib.error.HTTPError as err:
            return err.code

    def _chat(self, model):
        return self._post(
            "/v1/chat/completions", {"model": model, "messages": [{"role": "user", "content": "Task: x"}]}
        )

    def test_outage_fails_only_the_named_model_until_switched_off(self):
        self.assertEqual(self._chat("brk-outage"), 200)
        self.assertEqual(self._post("/outage", {"model": "brk-outage", "down": True}), 200)
        self.assertEqual(self._chat("brk-outage"), 503)
        self.assertEqual(self._chat("other"), 200)
        self.assertEqual(self._post("/outage", {"model": "brk-outage", "down": False}), 200)
        self.assertEqual(self._chat("brk-outage"), 200)

    def test_outage_rejects_a_malformed_body(self):
        self.assertEqual(self._post("/outage", {"model": "brk-outage"}), 400)
        self.assertEqual(self._post("/outage", {"down": True}), 400)
        self.assertFalse(server.in_outage("brk-outage"))


if __name__ == "__main__":
    unittest.main()
