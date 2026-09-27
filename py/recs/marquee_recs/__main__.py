"""Entry point. Serves a health endpoint until the gRPC service is implemented."""

import json
import os
import signal
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from marquee_recs import __version__


def main() -> None:
    port = int(os.environ.get("MARQUEE_RECS_PORT", "7720"))
    body = json.dumps({"status": "ok", "service": "recs", "version": __version__}).encode()

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
    print(f"recs {__version__} listening on :{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
