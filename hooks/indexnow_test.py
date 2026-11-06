"""How hooks/indexnow.py reads the live sitemap.

A real HTTP fetch, against a server that behaves like the site's edge: it answers
403 to Python's default User-Agent (`Python-urllib/3.x`) and 200 to any other.
That is what happened to the deploy that should have announced changed pages: the
fetch was refused, "submit nothing" is the safe answer, and the build stayed
green, so for as long as it went unnoticed no page was ever announced.

Run with: python3 hooks/indexnow_test.py (internal/docs runs it with the Go tests).
"""

from __future__ import annotations

import contextlib
import http.server
import io
import json
import os
import shutil
import sys
import tempfile
import threading
import unittest
import unittest.mock
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import indexnow  # noqa: E402

PAGE = "https://zoomies.sh/architecture/"


def sitemap(pages: dict[str, str]) -> str:
    rows = "".join(
        f"<url><loc>{loc}</loc><lastmod>{date}</lastmod></url>" for loc, date in pages.items()
    )
    return f'<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">{rows}</urlset>'


class LiveSitemap(unittest.TestCase):
    def serve(self, body: str):
        seen: list[str] = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                agent = self.headers.get("User-Agent", "")
                seen.append(agent)
                if agent.startswith("Python-urllib"):
                    self.send_response(403)
                    self.end_headers()
                    return
                self.send_response(200)
                self.send_header("Content-Type", "application/xml")
                self.end_headers()
                self.wfile.write(body.encode("utf-8"))

            def log_message(self, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        self.addCleanup(server.server_close)
        return f"http://127.0.0.1:{server.server_address[1]}/sitemap.xml", seen

    def run_main(self, built: dict[str, str], live_url: str) -> tuple[str, str]:
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, directory, ignore_errors=True)
        (directory / "sitemap.xml").write_text(sitemap(built), encoding="utf-8")
        output = directory / "github_output"
        printed = io.StringIO()
        with unittest.mock.patch.dict(os.environ, {"GITHUB_OUTPUT": str(output)}), contextlib.redirect_stdout(printed):
            indexnow.main(["indexnow.py", str(directory / "sitemap.xml"), live_url, "k3y"])
        return output.read_text(encoding="utf-8")[len("payload="):].strip(), printed.getvalue()

    def test_the_fetch_does_not_announce_itself_as_python_urllib(self):
        live_url, seen = self.serve(sitemap({PAGE: "2026-01-01"}))
        payload, _ = self.run_main({PAGE: "2026-02-01"}, live_url)
        self.assertNotEqual(payload, "", "the live sitemap was refused, so nothing was compared")
        self.assertEqual(json.loads(payload)["urlList"], [PAGE])
        self.assertFalse(any(agent.startswith("Python-urllib") for agent in seen), seen)

    def test_a_sitemap_that_cannot_be_read_is_a_workflow_warning(self):
        # Submitting nothing is right, but a green run that says so only in its
        # log is how the refusal above went unseen.
        live_url, _ = self.serve("")  # a 200 whose body is not a sitemap
        with unittest.mock.patch.dict(os.environ, {"GITHUB_ACTIONS": "true"}):
            _, printed = self.run_main({PAGE: "2026-02-01"}, live_url)
        self.assertIn("::warning::", printed)


if __name__ == "__main__":
    unittest.main()
