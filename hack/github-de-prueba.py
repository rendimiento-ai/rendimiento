#!/usr/bin/env python3
"""A stand-in for GitHub, for the quickstart's test (hack/ci-inicio-rapido.sh).

An installed rendimiento pointed at it (GITHUB_API_URL and GITHUB_URL) gets,
without a person or a browser, what the setup page and the sign-in would
have brought from GitHub: a GitHub App with a key made here, one
installation that sees one public repository, and a sign-in for LOGIN.
What rendimiento reads from that repository (its files, its commits) is
asked of the real GitHub, so the test deploys the example as it is today;
what it would write (checks, comments) is accepted and dropped.

The installation's token is empty: the repository is public, and the
clone, which goes to the real GitHub, needs none.

  github-de-prueba.py [address:port]      127.0.0.1:8099 by default
  REPO=rendimiento-ai/hola   the repository the installation sees
  LOGIN=octocat              who signs in
  GITHUB_TOKEN               optional, for the reads of the real GitHub
"""
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REPO = os.environ.get("REPO", "rendimiento-ai/hola")
LOGIN = os.environ.get("LOGIN", "octocat")
TOKEN = os.environ.get("GITHUB_TOKEN", "")
REAL = "https://api.github.com"
CUENTA = REPO.split("/")[0]
INSTALACION = {
    "id": 1,
    "account": {"login": CUENTA},
    "permissions": {"contents": "write", "pull_requests": "write", "checks": "write", "metadata": "read"},
}


def real(path):
    """The real GitHub's answer to a read: its status and its body."""
    req = urllib.request.Request(REAL + path, headers={"Accept": "application/vnd.github+json", "User-Agent": "rendimiento-inicio-rapido"})
    if TOKEN:
        req.add_header("Authorization", "Bearer " + TOKEN)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


class GitHub(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        sys.stderr.write("%s %s\n" % (datetime.now().strftime("%H:%M:%S"), fmt % args))

    def answer(self, status, body):
        if not isinstance(body, bytes):
            body = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/app":
            self.answer(200, {"id": 1, "slug": "rendimiento-de-prueba", "name": "rendimiento-de-prueba"})
        elif path == "/app/installations":
            self.answer(200, [INSTALACION])
        elif path == "/app/installations/1":
            self.answer(200, INSTALACION)
        elif path == "/installation/repositories":
            status, body = real("/repos/" + REPO)
            if status != 200:
                self.answer(status, body)
                return
            self.answer(200, {"total_count": 1, "repositories": [json.loads(body)]})
        elif path == "/user":
            self.answer(200, {"login": LOGIN})
        else:
            self.answer(*real(self.path))

    def write(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        path = self.path.split("?")[0]
        if path.startswith("/app-manifests/") and path.endswith("/conversions"):
            self.answer(201, {
                "id": 1, "slug": "rendimiento-de-prueba", "pem": self.server.llave,
                "webhook_secret": "de-prueba", "client_id": "de-prueba", "client_secret": "de-prueba",
                "html_url": "https://github.com/apps/rendimiento-de-prueba",
            })
        elif path == "/app/installations/1/access_tokens":
            expira = datetime.now(timezone.utc) + timedelta(hours=1)
            self.answer(201, {"token": "", "expires_at": expira.strftime("%Y-%m-%dT%H:%M:%SZ")})
        elif path == "/login/oauth/access_token":
            self.answer(200, {"access_token": "de-prueba"})
        else:
            # A check, a comment: nothing the test reads back.
            self.answer(201 if self.command == "POST" else 200, {"id": 1})

    do_POST = do_PATCH = do_PUT = do_DELETE = write


def main():
    host, _, port = (sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1:8099").rpartition(":")
    srv = ThreadingHTTPServer((host, int(port)), GitHub)
    srv.llave = subprocess.run(["openssl", "genrsa", "2048"], check=True, capture_output=True, text=True).stdout
    sys.stderr.write("a stand-in for GitHub at http://%s:%s, with %s\n" % (host, port, REPO))
    srv.serve_forever()


if __name__ == "__main__":
    main()
