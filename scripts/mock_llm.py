#!/usr/bin/env python3
"""Mock LLM server for agentd testing.

Responds to OpenAI-compatible chat completions API with simple echo responses.
Supports tool calls for task creation.

Usage: python3 mock_llm.py [--port PORT]
"""

import json
import sys
import time
import argparse
from http.server import HTTPServer, BaseHTTPRequestHandler


class MockLLMHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/v1/models":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            response = {
                "object": "list",
                "data": [
                    {
                        "id": "mock-model",
                        "object": "model",
                        "created": int(time.time()),
                        "owned_by": "mock",
                    }
                ],
            }
            self.wfile.write(json.dumps(response).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self.send_response(404)
            self.end_headers()
            return

        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length)

        try:
            request = json.loads(body)
        except json.JSONDecodeError:
            self.send_response(400)
            self.end_headers()
            return

        # Check if tools are requested
        tools = request.get("tools", [])
        messages = request.get("messages", [])
        tool_names = {
            tool.get("function", {}).get("name")
            for tool in tools
            if isinstance(tool, dict)
        }
        last_message = messages[-1] if messages and isinstance(messages[-1], dict) else {}
        last_role = last_message.get("role")

        if "create_task" in tool_names and last_role == "user":
            # Simulate a tool call response
            response = {
                "id": "chatcmpl-mock",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": request.get("model", "mock-model"),
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": None,
                            "tool_calls": [
                                {
                                    "id": "call_mock_1",
                                    "type": "function",
                                    "function": {
                                        "name": "create_task",
                                        "arguments": json.dumps({
                                            "title": "Mock task from chat",
                                            "description": "This is a test task created by the mock LLM",
                                        }),
                                    },
                                }
                            ],
                        },
                        "finish_reason": "tool_calls",
                    }
                ],
                "usage": {
                    "prompt_tokens": 100,
                    "completion_tokens": 50,
                    "total_tokens": 150,
                },
            }
        else:
            # Simple text response
            last_msg = last_message.get("content") or ""
            response = {
                "id": "chatcmpl-mock",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": request.get("model", "mock-model"),
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": f"Mock response to: {last_msg[:100]}",
                        },
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 100,
                    "completion_tokens": 20,
                    "total_tokens": 120,
                },
            }

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(response).encode())

    def log_message(self, format, *args):
        # Suppress default logging
        pass


def main():
    parser = argparse.ArgumentParser(description="Mock LLM server for agentd testing")
    parser.add_argument("--port", type=int, default=4000, help="Port to listen on")
    args = parser.parse_args()

    server = HTTPServer(("127.0.0.1", args.port), MockLLMHandler)
    print(f"Mock LLM server running on http://127.0.0.1:{args.port}", file=sys.stderr)
    print(f"OpenAI-compatible endpoint: http://127.0.0.1:{args.port}/v1/chat/completions", file=sys.stderr)
    sys.stderr.flush()
    server.serve_forever()


if __name__ == "__main__":
    main()