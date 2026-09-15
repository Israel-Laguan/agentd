#!/usr/bin/env python3
import json, os, sys, urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

port = None
log_path = None

def _normalize_url(base):
    return base.rstrip("/")

class H(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        with open(log_path, "a") as f:
            f.write("%s - %s\n" % (self.address_string(), fmt % args))
    def do_GET(self):
        body = b'{"ok":true,"role":"secondary-mock"}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0"))
        body = b""
        while n > 0:
            chunk = self.rfile.read(min(n, 65536))
            if not chunk:
                break
            body += chunk
            n -= len(chunk)
        payload = {"id":"chatcmpl-mock","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"fallback ok from secondary"},"finish_reason":"stop"}],"model":"mock-secondary"}
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

def run():
    global port, log_path
    port = int(os.environ["MOCK_PORT"])
    if not (1024 <= port <= 65535):
        print(f"MOCK_PORT must be 1024-65535, got {port}", file=sys.stderr)
        sys.exit(1)
    log_path = os.environ["MOCK_LOG"]
    if not log_path or ".." in log_path or not log_path.startswith("/"):
        print(f"MOCK_LOG must be an absolute path without '..': {log_path}", file=sys.stderr)
        sys.exit(1)
    HTTPServer(("127.0.0.1", port), H).serve_forever()

def api_status(api_url):
    try:
        return json.load(urllib.request.urlopen(_normalize_url(api_url) + "/api/v1/system/status", timeout=3))
    except Exception:
        return None

def api_projects(api_url):
    try:
        return json.load(urllib.request.urlopen(_normalize_url(api_url) + "/api/v1/projects", timeout=5))
    except Exception:
        return None

def api_tasks(api_url, project_id):
    try:
        import re
        if not re.match(r'^[a-zA-Z0-9_\-]+$', project_id):
            print(f"Invalid project_id: {project_id}", file=sys.stderr)
            return None
        return json.load(urllib.request.urlopen(f"{_normalize_url(api_url)}/api/v1/projects/{project_id}/tasks?include_healing=true", timeout=5))
    except Exception:
        return None

if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "server":
        run()
    elif len(sys.argv) > 1 and sys.argv[1] == "status":
        base = os.environ.get("API_URL", "http://127.0.0.1:18776")
        s = api_status(base)
        if s:
            print(json.dumps(s))
    elif len(sys.argv) > 1 and sys.argv[1] == "projects":
        base = os.environ.get("API_URL", "http://127.0.0.1:18776")
        p = api_projects(base)
        if p:
            for proj in p.get("data") or []:
                print(proj.get("name"), proj.get("id"))
                tasks = api_tasks(base, proj["id"])
                for t in (tasks.get("data", []) if tasks else []):
                    print(" ", t.get("state"), t.get("assignee"), t.get("title"))
