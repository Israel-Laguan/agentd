#!/usr/bin/env python3
"""Unit tests for the mock LLM's scenario dispatch and tiered replies.

Run with:  python3 -m unittest discover -s devenv/mockllm -p 'test_server.py'
(or `make test-mockllm` from the repo root).
"""

import json
import sys
import threading
import time
import unittest
import urllib.error
import urllib.request
from email.message import Message
from http.server import ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import server  # noqa: E402
import toolcalls  # noqa: E402


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


class SlowCommandTest(unittest.TestCase):
    def test_slow_command_reports_a_first_attempts_file(self):
        # J08 proves the recovery.clean_workspace_on_recover reset with this: the
        # command drops a marker file, and a re-run that still sees it says so.
        command = server.slow_command()
        self.assertIn("if [ -e attempt.marker ]; then echo carried-over; fi", command)
        self.assertLess(command.index("carried-over"), command.index("touch attempt.marker"))

    def test_slow_command_prints_every_tick_through_the_last(self):
        command = server.slow_command()
        self.assertIn(f"-lt {server.SLOW_TICKS}", command)
        self.assertIn("echo tick $i", command)


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

    def setUp(self):
        # The chats below are fixtures, not journey traffic. Keep them out of the
        # shared /tmp capture log so a later GET /requests consumer sees only its
        # own requests.
        self._real_record_request = server.record_request
        server.record_request = staticmethod(lambda body: None)

    def tearDown(self):
        server.record_request = self._real_record_request
        server.set_outage("brk-outage", False)
        server.set_outage("other", False)
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

    def test_outage_rejects_a_non_object_or_non_string_model(self):
        self.assertEqual(self._post("/outage", ["brk-outage"]), 400)
        self.assertEqual(self._post("/outage", {"model": ["brk-outage"], "down": True}), 400)
        self.assertEqual(self._post("/outage", {"model": "", "down": True}), 400)
        self.assertFalse(server.in_outage("brk-outage"))


class QuotaAndSlowOnceTest(unittest.TestCase):
    """The quota toggle and the one-shot delay, both keyed by model name.

    Quota exists because only ErrLLMQuotaExceeded feeds the per-provider
    breakers; an outage is an unreachable provider and feeds the global one. A
    journey that wants to exercise a provider breaker's probe slot needs 429s.
    slow_once exists so a journey can hold a probe in flight long enough to see
    what the siblings do while it runs.
    """

    @classmethod
    def setUpClass(cls):
        cls.httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        cls.base = f"http://127.0.0.1:{cls.httpd.server_port}"
        threading.Thread(target=cls.httpd.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()
        cls.httpd.server_close()

    def setUp(self):
        # Fixture chats, not journey traffic: keep them out of the shared capture
        # log so a later GET /requests consumer sees only its own requests.
        self._real_record_request = server.record_request
        server.record_request = staticmethod(lambda body: None)
        server.set_quota("prb-quota", False)
        server.set_slow_once("prb-quota", 0)

    def tearDown(self):
        server.record_request = self._real_record_request
        server.set_quota("prb-quota", False)
        server.set_slow_once("prb-quota", 0)

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

    def test_quota_answers_429_and_only_for_the_named_model(self):
        self.assertEqual(self._post("/quota", {"model": "prb-quota", "on": True}), 200)
        self.assertEqual(self._chat("prb-quota"), 429)
        self.assertEqual(self._chat("other"), 200)
        self.assertEqual(self._post("/quota", {"model": "prb-quota", "on": False}), 200)
        self.assertEqual(self._chat("prb-quota"), 200)

    def test_quota_is_distinct_from_an_outage(self):
        """A quota failure and an outage must not be the same status: they reach
        different breakers, so collapsing them would silently retarget the test."""
        self._post("/quota", {"model": "prb-quota", "on": True})
        self.assertEqual(self._chat("prb-quota"), 429)
        self.assertNotEqual(server.QUOTA_STATUS, server.OUTAGE_STATUS)

    def test_quota_rejects_a_malformed_body(self):
        self.assertEqual(self._post("/quota", {"model": "prb-quota"}), 400)
        self.assertEqual(self._post("/quota", {"on": True}), 400)
        self.assertEqual(self._post("/quota", {"model": "prb-quota", "on": "yes"}), 400)
        self.assertFalse(server.in_quota("prb-quota"))

    def test_slow_once_delays_exactly_one_request(self):
        self.assertEqual(self._post("/slow_once", {"model": "prb-quota", "seconds": 0.4}), 200)
        started = time.monotonic()
        self.assertEqual(self._chat("prb-quota"), 200)
        first = time.monotonic() - started
        started = time.monotonic()
        self.assertEqual(self._chat("prb-quota"), 200)
        second = time.monotonic() - started
        self.assertGreater(first, 0.3, "the armed request was not delayed")
        self.assertLess(second, 0.3, "the delay was not consumed by a single request")

    def test_slow_once_rejects_a_malformed_body(self):
        self.assertEqual(self._post("/slow_once", {"model": "prb-quota"}), 400)
        self.assertEqual(self._post("/slow_once", {"model": "prb-quota", "seconds": "soon"}), 400)
        self.assertEqual(self._post("/slow_once", {"model": "prb-quota", "seconds": -1}), 400)

    def test_slow_once_does_not_mask_a_quota_failure(self):
        """An armed delay must not turn a failing provider into a slow success:
        the failure has to be reported before the sleep."""
        self._post("/quota", {"model": "prb-quota", "on": True})
        self._post("/slow_once", {"model": "prb-quota", "seconds": 5})
        started = time.monotonic()
        self.assertEqual(self._chat("prb-quota"), 429)
        self.assertLess(time.monotonic() - started, 2.0, "the quota failure waited out the armed delay")


class AgenticDefaultReplyTest(unittest.TestCase):
    """B-080: the default profile is agentic. An untagged worker request that
    offers tools gets one bash call for the usual command, then the command's
    output as the final message; never the command JSON as plain text."""

    TOOLS = [{"type": "function", "function": {"name": "bash"}}]

    def _body(self, title="build a thing", tool_result=None):
        messages = [
            {"role": "system", "content": "You are an agent."},
            {"role": "user", "content": f"You are executing Task: {title}"},
        ]
        if tool_result is not None:
            messages += [
                {"role": "assistant", "content": None, "tool_calls": [{"id": "c0"}]},
                {"role": "tool", "tool_call_id": "c0", "content": tool_result},
            ]
        return {"model": "agentd", "messages": messages, "tools": self.TOOLS}

    def _call_args(self, reply):
        choice = reply["choices"][0]
        self.assertEqual(choice["finish_reason"], "tool_calls")
        call = choice["message"]["tool_calls"][0]
        self.assertEqual(call["function"]["name"], "bash")
        return json.loads(call["function"]["arguments"])

    def test_first_turn_runs_the_usual_command_through_bash(self):
        args = self._call_args(server.chat_completion(self._body()))
        self.assertEqual(args["command"], server.build_command("unknown", "build a thing"))

    def test_slow_task_runs_the_slow_command_through_bash(self):
        args = self._call_args(server.chat_completion(self._body(title=f"{server.SLOW_MARKER} recover")))
        self.assertEqual(args["command"], server.slow_command())

    def test_final_message_carries_the_command_output(self):
        reply = server.chat_completion(self._body(tool_result="tick 28\ntick 29"))
        choice = reply["choices"][0]
        self.assertEqual(choice["finish_reason"], "stop")
        self.assertIn("tick 29", choice["message"]["content"])

    def test_request_without_tools_keeps_the_command_json(self):
        body = self._body()
        del body["tools"]
        self.assertIn("command", json.loads(server.chat_completion(body)["choices"][0]["message"]["content"]))


class ToolCallScenarioTest(unittest.TestCase):
    """T-053: a task can script `tool_calls` for the agentic loop."""

    CALLS = [{"name": "bash", "arguments": {"command": "sudo id"}}]

    def _body(self, calls=None, tools=True, tool_result=None):
        tag = toolcalls.encode_tool_calls(self.CALLS if calls is None else calls)
        messages = [
            {"role": "system", "content": "You are an agent."},
            {"role": "user", "content": f"Task: scripted\n@tool_calls={tag}"},
        ]
        if tool_result is not None:
            messages += [
                {"role": "assistant", "content": None, "tool_calls": [{"id": "c0"}]},
                {"role": "tool", "tool_call_id": "c0", "content": tool_result},
            ]
        body = {"model": "agentd", "messages": messages}
        if tools:
            body["tools"] = [{"type": "function", "function": {"name": "bash"}}]
        return body

    def test_first_turn_returns_the_configured_tool_calls(self):
        reply = server.chat_completion(self._body())
        choice = reply["choices"][0]
        self.assertEqual(choice["finish_reason"], "tool_calls")
        calls = choice["message"]["tool_calls"]
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["type"], "function")
        self.assertEqual(calls[0]["function"]["name"], "bash")
        self.assertEqual(json.loads(calls[0]["function"]["arguments"]), {"command": "sudo id"})

    def test_next_turn_is_a_final_message(self):
        reply = server.chat_completion(self._body(tool_result="refused"))
        choice = reply["choices"][0]
        self.assertEqual(choice["finish_reason"], "stop")
        self.assertEqual(choice["message"]["content"], toolcalls.FINAL_MESSAGE)
        self.assertNotIn("tool_calls", choice["message"])

    def test_request_without_tools_keeps_the_default_reply(self):
        reply = server.chat_completion(self._body(tools=False))
        self.assertIn("command", json.loads(reply["choices"][0]["message"]["content"]))

    def test_malformed_tag_keeps_the_default_reply(self):
        # The default for a request that offers tools is the untagged agentic
        # reply: one bash call for the usual command (B-080).
        body = self._body()
        body["messages"][1]["content"] = "Task: scripted\n@tool_calls=!!!not-base64"
        reply = server.chat_completion(body)
        call = reply["choices"][0]["message"]["tool_calls"][0]
        self.assertEqual(call["function"]["name"], "bash")
        self.assertNotIn("sudo id", call["function"]["arguments"])
        self.assertEqual(toolcalls.configured_tool_calls(self._body(calls=[{"arguments": {}}])), [])

    def test_non_object_arguments_keep_the_default_reply(self):
        for bad in (None, [], "sudo id", 3):
            calls = [{"name": "bash", "arguments": bad}]
            self.assertEqual(toolcalls.configured_tool_calls(self._body(calls=calls)), [], bad)
        self.assertEqual(len(toolcalls.configured_tool_calls(self._body(calls=[{"name": "bash"}]))), 1)

    def test_tool_call_reply_is_served_over_http(self):
        httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        threading.Thread(target=httpd.serve_forever, daemon=True).start()
        self.addCleanup(httpd.server_close)
        self.addCleanup(httpd.shutdown)
        req = urllib.request.Request(
            f"http://127.0.0.1:{httpd.server_address[1]}/v1/chat/completions",
            data=json.dumps(self._body()).encode(),
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req) as resp:
            reply = json.load(resp)
        self.assertEqual(reply["choices"][0]["message"]["tool_calls"][0]["function"]["name"], "bash")


if __name__ == "__main__":
    unittest.main()
