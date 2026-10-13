"""Opt-in importer drill using generated images and disposable PostgreSQL.

Uses KURA_BACKUP_TEST_ADMIN_URL and the existing isolated API fixture helper.
Never reads a real source folder or the development library. A forwarding
proxy drops one committed upload response to prove reconciliation and resume.
"""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError
from urllib.request import Request, urlopen


HELPER = Path(__file__).with_name("test_library_backup_integration.py")
SPEC = importlib.util.spec_from_file_location("backup_api_fixture", HELPER)
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)
IMPORTER = Path(__file__).with_name("import-test-media.py")


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL to run the importer drill")
class ImporterDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)
        self.folder = self.fixture.root / "source-folder"
        self.folder.mkdir()
        self.manifest = self.fixture.root / "resume.json"
        self.posts = 0
        self.drop_next = True
        self.repeat_cursors = False
        owner = self

        class Proxy(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.forward()

            def do_POST(self):
                self.forward()

            def forward(self):
                if owner.repeat_cursors and self.path.startswith("/api/posts?"):
                    cursor = "cursor=" in self.path
                    body = json.dumps({"posts": [{"id": "2" if cursor else "1"}], "next_cursor": "repeated"}).encode()
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return
                length = int(self.headers.get("Content-Length", "0"))
                data = self.rfile.read(length) if length else None
                headers = {key: value for key, value in self.headers.items() if key.lower() not in {"host", "connection", "content-length"}}
                try:
                    response = urlopen(Request(owner.api + self.path, data=data, headers=headers, method=self.command), timeout=5)
                except HTTPError as response_error:
                    response = response_error
                with response:
                    body = response.read()
                    status = response.status
                    if self.command == "POST" and self.path == "/api/uploads":
                        owner.posts += 1
                        if owner.drop_next and status == 201:
                            owner.drop_next = False
                            # The actual Kura API committed before its response
                            # disappeared; the importer must not resend this file.
                            self.close_connection = True
                            self.connection.shutdown(socket.SHUT_RDWR)
                            return
                    self.send_response(status)
                    self.send_header("Content-Type", response.headers.get("Content-Type", "application/json"))
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)

        self.proxy = ThreadingHTTPServer(("127.0.0.1", 0), Proxy)
        self.proxy.daemon_threads = True
        self.thread = threading.Thread(target=self.proxy.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_proxy)
        self.proxy_url = "http://127.0.0.1:" + str(self.proxy.server_port)

    def stop_proxy(self):
        self.proxy.shutdown()
        self.proxy.server_close()
        self.thread.join(timeout=5)

    def run_importer(self, manifest=None, token="restore-drill-token", success=True):
        env = os.environ.copy()
        env["KURA_API_TOKEN"] = token
        result = subprocess.run([sys.executable, str(IMPORTER), str(self.folder), "--api-url", self.proxy_url, "--manifest", str(manifest or self.manifest)], env=env, capture_output=True, text=True)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        else:
            self.assertNotEqual(result.returncode, 0, "unsafe import unexpectedly succeeded")
        return result

    def test_committed_lost_response_resume_and_fail_closed_guards(self):
        (self.folder / "a.png").write_bytes(FIXTURE.png((12, 35, 90)))
        (self.folder / "b-copy.png").write_bytes(FIXTURE.png((12, 35, 90)))
        (self.folder / "c.png").write_bytes(FIXTURE.png((99, 20, 75)))
        (self.folder / ".hidden.png").write_bytes(FIXTURE.png((1, 2, 3)))
        hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in self.folder.iterdir()}
        self.run_importer()
        self.assertEqual(self.posts, 2, "two unique files must produce exactly two upload requests despite the lost response")
        entries = json.loads(self.manifest.read_text())["files"]
        self.assertEqual(entries["a.png"]["status"], "confirmed-after-uncertain-response")
        self.assertEqual(entries["b-copy.png"]["status"], "duplicate")
        self.assertEqual(entries["c.png"]["status"], "imported")
        self.assertNotIn(".hidden.png", entries)
        self.assertEqual(hashes, {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in self.folder.iterdir()}, "source bytes must stay unchanged")
        self.run_importer()
        self.assertEqual(self.posts, 2, "resuming must not upload a confirmed file again")
        rows = self.fixture.request(self.api, "GET", "/api/posts?q=tag%3Afolder-test")["posts"]
        self.assertEqual(len(rows), 2)
        for row in rows:
            self.assertEqual(set(row["tags"]), {"folder-test", "test-data"})

        wrong_token_manifest = self.fixture.root / "wrong-token.json"
        self.run_importer(manifest=wrong_token_manifest, token="invalid-token", success=False)
        self.assertEqual(self.posts, 2, "a locked importer must not send upload bodies")
        self.assertFalse(wrong_token_manifest.exists())

        corrupt = self.fixture.root / "corrupt-manifest.json"
        corrupt.write_bytes(b"not valid JSON")
        self.run_importer(manifest=corrupt, success=False)
        self.assertEqual(corrupt.read_bytes(), b"not valid JSON")
        self.assertEqual(self.posts, 2)

        self.repeat_cursors = True
        self.run_importer(manifest=self.fixture.root / "loop-manifest.json", success=False)
        self.assertEqual(self.posts, 2, "a broken browse cursor must fail before writes")
        self.repeat_cursors = False

        (self.folder / "a.png").write_bytes(FIXTURE.png((10, 200, 35)))
        result = self.run_importer(success=False)
        self.assertEqual(result.returncode, 2, "a changed prior upload stays unresolved")
        self.assertEqual(self.posts, 2)


if __name__ == "__main__":
    unittest.main()
