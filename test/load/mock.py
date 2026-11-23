#!/usr/bin/env python3
"""Offline check of api.js: a stand-in for the portal API that answers every
call the script makes, and a dry k6 run against it. No lab, no secrets.

  test/load/mock.py    # exit 0 when k6 runs clean against a sound stand-in
                       # and stops with 99 against one that answers 500
"""
import json
import os
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

IMAGE = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "k6-image")).read().strip()
seen = {}
broken = False


class API(BaseHTTPRequestHandler):
    def answer(self, status, body):
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def handle_one(self):
        if broken:
            return self.answer(500, {"code": "INTERNAL"})
        if self.headers.get("Cookie") != "s=1" or self.headers.get("X-CSRF-Token") != "c":
            return self.answer(401, {"code": "UNAUTHENTICATED"})
        n = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(n)) if n else None
        key = f"{self.command} {self.path.split('/')[3] if self.path.count('/') > 3 else self.path}"
        seen[key] = seen.get(key, 0) + 1
        op = {"id": "op-1", "type": "workspace.update", "state": "Running"}
        if self.command == "GET":
            return self.answer(200, {**op, "state": "Succeeded"} if "/operations/" in self.path else {"items": []})
        if self.command == "PUT":
            return self.answer(202, {"items": [], "operation": op})
        if self.command == "POST":
            if not self.headers.get("Idempotency-Key"):
                return self.answer(400, {"code": "VALIDATION_FAILED"})
            return self.answer(202, {"namespace": {"name": f"o-w-{body['name']}"}, "operation": op})
        return self.answer(202, {"namespace": {"name": "x"}, "operation": op})

    do_GET = do_PUT = do_POST = do_DELETE = handle_one

    def log_message(self, *args):
        pass


def k6(port):
    fixture = {"portal": f"http://127.0.0.1:{port}", "org": "o", "toggleGroupId": "g",
               "users": [{"cookie": "s=1", "csrf": "c"}], "workspaces": [{"id": "w", "operationId": "op-0"}]}
    with tempfile.TemporaryDirectory() as data:
        # As load.spec.ts passes the sessions: a private file, k6 as this user.
        with open(os.open(os.path.join(data, "fixture.json"), os.O_WRONLY | os.O_CREAT, 0o600), "w") as f:
            json.dump(fixture, f)
        here = os.path.dirname(os.path.abspath(__file__))
        return subprocess.run(["docker", "run", "--rm", "--network", "host", "--user", f"{os.getuid()}:{os.getgid()}",
                               "-v", f"{data}:/data:ro",
                               "-v", f"{here}:/scripts:ro", "-e", "MODE=dry", IMAGE, "run", "--quiet",
                               "/scripts/api.js"], capture_output=True, text=True).returncode


def main():
    global broken
    server = ThreadingHTTPServer(("127.0.0.1", 0), API)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    sound = k6(server.server_port)
    print("calls:", json.dumps(seen, sort_keys=True))
    broken = True
    failing = k6(server.server_port)
    server.shutdown()
    # Reads, the toggle and the operation polls were all reached, and the
    # error threshold fails the run (k6 exit 99).
    ok = sound == 0 and all(seen.get(k) for k in ("GET /api/v1/me", "GET operations", "PUT workspaces")) and failing == 99
    print("PASS" if ok else f"FAIL (k6 exit {sound} against the sound stand-in, {failing} against the failing one)")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
