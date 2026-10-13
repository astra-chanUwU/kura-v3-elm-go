"""Fake-HTTP tests for the ordinary, option-bound folder importer."""

import contextlib
import hashlib
import importlib.util
import io
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import tempfile
import threading
import unittest
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlsplit


SCRIPT = Path(__file__).with_name("import-media.py")
SPEC = importlib.util.spec_from_file_location("import_media", SCRIPT)
media = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(media)


def sha(data):
    return hashlib.sha256(data).hexdigest()


class State:
    def __init__(self):
        self.capabilities = {"writes_require_auth": False, "can_write": True, "actor": "local"}
        self.visible = {}
        self.requests = []
        self.uploads = []
        self.attachments = []
        self.collection_id = "9"
        self.lose_next_attach = False
        self.lose_next_upload = False
        self.upload_status = 201
        self.next_post_id = 100


class FakeAPI:
    def __init__(self, state):
        self.state = state
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def respond(self, status, value):
                body = json.dumps(value).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_GET(self):
                state.requests.append(("GET", self.path, self.headers.get("Authorization")))
                parsed = urlsplit(self.path)
                if parsed.path == "/health":
                    return self.respond(200, {"status": "ok"})
                if parsed.path == "/api/capabilities":
                    return self.respond(200, state.capabilities)
                if parsed.path == "/api/collections/summaries":
                    rows = []
                    if state.collection_id:
                        rows.append({"id": state.collection_id, "name": "Destination", "version": 0,
                                     "member_count": len(state.attachments), "visible_post_count": len(state.attachments)})
                    return self.respond(200, {"collections": rows})
                if parsed.path == "/api/posts":
                    ids = list(state.visible.values())
                    return self.respond(200, {"posts": [{"id": post_id} for post_id in ids], "next_cursor": None})
                if parsed.path.startswith("/api/posts/"):
                    post_id = parsed.path.rsplit("/", 1)[-1]
                    for digest, known_id in state.visible.items():
                        if known_id == post_id:
                            return self.respond(200, {"id": post_id, "hash": "sha256:" + digest})
                    return self.respond(200, {"id": post_id, "hash": ""})
                return self.respond(404, {"error": "missing"})

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                body = self.rfile.read(length)
                state.requests.append(("POST", self.path, self.headers.get("Authorization")))
                if self.path == "/api/uploads":
                    state.uploads.append(body)
                    if state.upload_status != 201:
                        return self.respond(state.upload_status, {"error": "rejected"})
                    state.next_post_id += 1
                    post_id = str(state.next_post_id)
                    digest = state.upload_hash
                    state.visible[digest] = post_id
                    if state.lose_next_upload:
                        state.lose_next_upload = False
                        self.close_connection = True
                        try:
                            self.connection.shutdown(2)
                        except OSError:
                            pass
                        self.connection.close()
                        return
                    self.respond(201, {"id": post_id, "hash": "sha256:" + digest})
                    return
                if self.path == f"/api/collections/{state.collection_id}/posts":
                    request = json.loads(body)
                    ids = request.get("post_ids", [])
                    state.attachments.extend(item for item in ids if item not in state.attachments)
                    if state.lose_next_attach:
                        state.lose_next_attach = False
                        self.close_connection = True
                        try:
                            self.connection.shutdown(2)
                        except OSError:
                            pass
                        self.connection.close()
                        return
                    return self.respond(200, {"id": state.collection_id, "name": "Destination",
                                              "version": 1, "post_ids": state.attachments})
                return self.respond(404, {"error": "missing"})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = f"http://127.0.0.1:{self.server.server_port}"

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


class ImportMediaTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.folder = self.root / "photos"
        (self.folder / "nested").mkdir(parents=True)
        self.image = self.folder / "nested" / "picture.jpg"
        self.data = b"ordinary source image bytes\x00"
        self.image.write_bytes(self.data)
        self.digest = sha(self.data)
        self.manifest = self.root / "resume.json"

    def invoke(self, api, *extra):
        return media.main([str(self.folder), "--api-url", api.url, "--manifest", str(self.manifest), *extra])

    def test_ordinary_upload_metadata_and_v2_manifest(self):
        state = State()
        state.upload_hash = self.digest
        with FakeAPI(state) as api:
            with contextlib.redirect_stdout(io.StringIO()):
                code = self.invoke(api, "--tag", "landscape", "--tag", "personal", "--artist", "Artist A")
        self.assertEqual(code, 0)
        body = state.uploads[0]
        self.assertIn(b'name="tags"\r\n\r\nlandscape', body)
        self.assertIn(b'name="tags"\r\n\r\npersonal', body)
        self.assertIn(b'name="artist"\r\n\r\nArtist A', body)
        self.assertIn(b'name="source"\r\n\r\nfolder-import/nested/picture.jpg', body)
        self.assertNotIn(b"test-data", body)
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest["version"], 2)
        self.assertEqual(manifest["options"]["tags"], ["landscape", "personal"])
        self.assertEqual(manifest["files"]["nested/picture.jpg"]["status"], "imported")

    def test_dry_run_previews_without_token_manifest_lock_or_writes(self):
        state = State()
        state.capabilities = {"writes_require_auth": True, "can_write": False, "actor": None}
        with FakeAPI(state) as api:
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                code = self.invoke(api, "--dry-run")
        self.assertEqual(code, 0)
        self.assertIn("would import: nested/picture.jpg", output.getvalue())
        self.assertIn("Preview:", output.getvalue())
        self.assertFalse(self.manifest.exists())
        self.assertFalse(Path(str(self.manifest) + ".lock").exists())
        self.assertEqual(state.uploads, [])
        self.assertFalse(any(method == "POST" for method, _, _ in state.requests))
        self.assertFalse(any(path == "/api/capabilities" for _, path, _ in state.requests))

    def test_dry_run_marks_same_folder_hashes_as_duplicates(self):
        duplicate = self.folder / "same-bytes.jpg"
        duplicate.write_bytes(self.data)
        state = State()
        with FakeAPI(state) as api:
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                code = self.invoke(api, "--dry-run")
        self.assertEqual(code, 0)
        self.assertIn("would import: nested/picture.jpg", output.getvalue())
        self.assertIn("duplicate (skip): same-bytes.jpg", output.getvalue())
        self.assertIn("Preview: 1 to import; 1 duplicates", output.getvalue())

    def test_invalid_destination_collection_stops_before_upload_or_manifest(self):
        state = State()
        state.collection_id = "8"
        with FakeAPI(state) as api:
            output = io.StringIO()
            with contextlib.redirect_stderr(output):
                code = self.invoke(api, "--collection", "9")
        self.assertEqual(code, 2)
        self.assertIn("Destination collection 9 was not found", output.getvalue())
        self.assertEqual(state.uploads, [])
        self.assertFalse(self.manifest.exists())

    def test_duplicate_skip_records_visible_id_and_error_policy_uploads_nothing(self):
        state = State()
        state.visible[self.digest] = "42"
        with FakeAPI(state) as api:
            with contextlib.redirect_stdout(io.StringIO()):
                code = self.invoke(api)
        self.assertEqual(code, 0)
        self.assertEqual(state.uploads, [])
        manifest = json.loads(self.manifest.read_text())
        entry = manifest["files"]["nested/picture.jpg"]
        self.assertEqual((entry["status"], entry["post_id"]), ("duplicate", "42"))

        state2 = State()
        state2.visible[self.digest] = "42"
        with FakeAPI(state2) as api:
            output = io.StringIO()
            with contextlib.redirect_stderr(output):
                code = media.main([str(self.folder), "--api-url", api.url, "--manifest", str(self.root / "error.json"),
                                   "--duplicate-policy", "error"])
        self.assertEqual(code, 2)
        self.assertIn("Duplicate content found", output.getvalue())
        self.assertEqual(state2.uploads, [])
        self.assertFalse((self.root / "error.json").exists())

    def test_duplicate_error_preflights_entire_folder_before_first_upload(self):
        earlier = self.folder / "a-new.jpg"
        earlier.write_bytes(b"new content before duplicate alphabetically")
        state = State()
        state.visible[self.digest] = "42"
        with FakeAPI(state) as api:
            with contextlib.redirect_stderr(io.StringIO()):
                code = self.invoke(api, "--duplicate-policy", "error")
        self.assertEqual(code, 2)
        self.assertEqual(state.uploads, [])
        self.assertFalse(self.manifest.exists())

    def test_duplicate_skip_attaches_existing_post_id(self):
        state = State()
        state.visible[self.digest] = "42"
        with FakeAPI(state) as api:
            with contextlib.redirect_stdout(io.StringIO()):
                code = self.invoke(api, "--collection", "9")
        self.assertEqual(code, 0)
        self.assertEqual(state.uploads, [])
        self.assertEqual(state.attachments, ["42"])
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest["attachment"]["attached_ids"], ["42"])

    def test_lost_attach_response_resumes_attach_without_reupload(self):
        state = State()
        state.upload_hash = self.digest
        state.lose_next_attach = True
        with FakeAPI(state) as api:
            with contextlib.redirect_stderr(io.StringIO()):
                first = self.invoke(api, "--collection", "9")
            self.assertEqual(first, 2)
            saved = json.loads(self.manifest.read_text())
            self.assertEqual(saved["files"]["nested/picture.jpg"]["status"], "imported")
            self.assertEqual(saved["attachment"]["state"], "uncertain")
            self.assertEqual(saved["attachment"]["post_ids"], ["101"])
            with contextlib.redirect_stdout(io.StringIO()):
                second = self.invoke(api, "--collection", "9")
        self.assertEqual(second, 0)
        self.assertEqual(len(state.uploads), 1)
        self.assertEqual(state.attachments, ["101"])
        self.assertEqual(sum(1 for method, path, _ in state.requests if method == "POST" and path.endswith("/posts")), 2)
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest["attachment"]["state"], "attached")
        self.assertEqual(manifest["attachment"]["post_ids"], [])
        self.assertEqual(manifest["attachment"]["attached_ids"], ["101"])

    def test_duplicate_error_rejects_resume_before_pending_attachment_write(self):
        state = State()
        state.upload_hash = self.digest
        state.lose_next_attach = True
        with FakeAPI(state) as api:
            with contextlib.redirect_stderr(io.StringIO()):
                first = self.invoke(api, "--collection", "9", "--duplicate-policy", "error")
            self.assertEqual(first, 2)
            self.assertEqual(len(state.attachments), 1)  # the first attach committed, its response was lost
            duplicate = self.folder / "later-duplicate.jpg"
            duplicate.write_bytes(self.data)
            with contextlib.redirect_stderr(io.StringIO()) as stderr:
                second = self.invoke(api, "--collection", "9", "--duplicate-policy", "error")
        self.assertEqual(second, 2)
        self.assertIn("Duplicate content found", stderr.getvalue())
        self.assertEqual(sum(1 for method, path, _ in state.requests if method == "POST" and path.endswith("/posts")), 1)
        self.assertEqual(len(state.uploads), 1)

    def test_lost_upload_response_reconciles_by_visible_hash_without_retry(self):
        state = State()
        state.upload_hash = self.digest
        state.lose_next_upload = True
        with FakeAPI(state) as api:
            with contextlib.redirect_stdout(io.StringIO()):
                first = self.invoke(api)
            with contextlib.redirect_stdout(io.StringIO()):
                second = self.invoke(api)
        self.assertEqual((first, second), (0, 0))
        self.assertEqual(len(state.uploads), 1)
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest["files"]["nested/picture.jpg"]["status"], "confirmed-after-uncertain-response")
        self.assertEqual(manifest["files"]["nested/picture.jpg"]["post_id"], "101")

    def test_upload_401_stops_before_later_files(self):
        (self.folder / "later.jpg").write_bytes(b"later image")
        state = State()
        state.upload_status = 401
        with FakeAPI(state) as api:
            with contextlib.redirect_stdout(io.StringIO()):
                code = self.invoke(api)
        self.assertEqual(code, 1)
        self.assertEqual(len(state.uploads), 1)

    def test_option_mismatch_leaves_manifest_bytes_unchanged(self):
        state = State()
        state.upload_hash = self.digest
        with FakeAPI(state) as api:
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(self.invoke(api, "--tag", "one"), 0)
            before = self.manifest.read_bytes()
            with contextlib.redirect_stderr(io.StringIO()):
                code = self.invoke(api, "--tag", "two")
        self.assertEqual(code, 2)
        self.assertEqual(self.manifest.read_bytes(), before)
        self.assertEqual(len(state.uploads), 1)

    def test_legacy_v1_manifest_is_rejected_without_replacement(self):
        legacy = {"version": 1, "source_root": str(self.folder.resolve()), "api_url": "unused", "files": {}}
        self.manifest.write_text(json.dumps(legacy))
        before = self.manifest.read_bytes()
        state = State()
        state.upload_hash = self.digest
        with FakeAPI(state) as api:
            with contextlib.redirect_stderr(io.StringIO()):
                code = self.invoke(api)
        self.assertEqual(code, 2)
        self.assertEqual(self.manifest.read_bytes(), before)
        self.assertEqual(state.uploads, [])


if __name__ == "__main__":
    unittest.main()
