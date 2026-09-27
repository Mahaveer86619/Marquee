"""Minimal standard-library health server, used until the gRPC service exists."""

import json
import signal
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def serve(service: str, version: str, port: int) -> None:
    body = json.dumps({"status": "ok", "service": service, "version": version}).encode()

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802 (stdlib naming)
            if self.path != "/healthz":
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_args) -> None:
            pass

    server = ThreadingHTTPServer(("0.0.0.0", port), Handler)
    signal.signal(signal.SIGTERM, lambda *_: server.shutdown())
    print(f"{service} {version} listening on :{port}", flush=True)
    server.serve_forever()
