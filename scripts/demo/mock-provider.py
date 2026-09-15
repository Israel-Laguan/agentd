#!/usr/bin/env python3
import json, os, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(os.environ["MOCK_PORT"])
log_path = os.environ["MOCK_LOG"]

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
        if n:
            self.rfile.read(n)
        payload = {"id":"chatcmpl-mock","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"fallback ok from secondary"},"finish_reason":"stop"}],"model":"mock-secondary"}
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

def run():
    HTTPServer(("127.0.0.1", port), H).serve_forever()

def api_status(api_url):
    import urllib.request
    try:
        return json.load(urllib.request.urlopen(api_url + "/api/v1/system/status", timeout=3))
    except Exception:
        return None

def api_projects(api_url):
    try:
        return json.load(urllib.request.urlopen(api_url + "/api/v1/projects", timeout=5))
    except Exception:
        return None

def api_tasks(api_url, project_id):
    try:
        return json.load(urllib.request.urlopen(f"{api_url}/api/v1/projects/{project_id}/tasks?include_healing=true", timeout=5))
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
                for t in api_tasks(base, proj["id"]).get("data", []):
                    print(" ", t.get("state"), t.get("assignee"), t.get("title"))
