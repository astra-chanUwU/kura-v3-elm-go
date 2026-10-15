"""Fake-HTTP and temporary-source tests for import-test-media.py."""

import hashlib
import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest import mock
from urllib.parse import parse_qs, urlsplit


SCRIPT = Path(__file__).with_name("import-test-media.py")
SPEC = importlib.util.spec_from_file_location("import_test_media", SCRIPT)
importer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(importer)


def sha(value):
    return hashlib.sha256(value).hexdigest()


class FakeState:
    def __init__(self):
        self.pages = [( [], None )]
        self.details = {}
        self.capabilities = {"writes_require_auth": False, "can_write": True, "actor": "local"}
        self.redirect_capabilities = False
        self.redirect_visits = 0
        self.requests = []
        self.posts = []
        self.post_mode = "success"
        self.upload_hash = None
        self.post_status = 201


class FakeKura:
    def __init__(self, state=None):
        self.state = state or FakeState()
        state = self.state

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def respond(self, code, data, headers=None):
                payload = json.dumps(data).encode("utf-8")
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                for key, value in (headers or {}).items():
                    self.send_header(key, value)
                self.end_headers()
                self.wfile.write(payload)

            def do_GET(self):
                self.server.state.requests.append(("GET", self.path, self.headers.get("Authorization")))
                if self.path == "/health":
                    return self.respond(200, {"status": "ok"})
                if self.path == "/api/capabilities":
                    if self.server.state.redirect_capabilities:
                        self.send_response(302)
                        self.send_header("Location", "/capture")
                        self.end_headers()
                        return
                    return self.respond(200, self.server.state.capabilities)
                if self.path == "/capture":
                    self.server.state.redirect_visits += 1
                    return self.respond(200, {"unexpected": True})
                parsed = urlsplit(self.path)
                if parsed.path == "/api/posts":
                    query = parse_qs(parsed.query)
                    cursor = query.get("cursor", [None])[0]
                    pages = self.server.state.pages
                    if callable(pages):
                        result = pages(cursor)
                    elif cursor is None:
                        result = pages[0]
                    else:
                        cursor_map = self.server.state.cursor_map
                        result = cursor_map[cursor]
                    if isinstance(result, dict):
                        return self.respond(200, result)
                    return self.respond(200, {"posts": result[0], "next_cursor": result[1]})
                if parsed.path.startswith("/api/posts/"):
                    post_id = parsed.path.rsplit("/", 1)[-1]
                    return self.respond(200, self.server.state.details.get(post_id, {}))
                return self.respond(404, {"error": "missing"})

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                body = self.rfile.read(length)
                current = self.server.state
                current.requests.append(("POST", self.path, self.headers.get("Authorization")))
                current.posts.append(body)
                if current.post_mode == "lost":
                    current.pages = [([{"id": "777"}], None)]
                    current.details["777"] = {"id": "777", "hash": "sha256:" + current.upload_hash}
                    self.close_connection = True
                    try:
                        self.connection.shutdown(2)
                    except OSError:
                        pass
                    self.connection.close()
                    return
                if current.post_mode == "redirect":
                    self.send_response(302)
                    self.send_header("Location", "/capture")
                    self.end_headers()
                    return
                if current.post_status != 201:
                    return self.respond(current.post_status, {"error": "rejected"})
                return self.respond(201, {"id": "100", "hash": "sha256:" + (current.upload_hash or "0" * 64)})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.state = state
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


class ImporterTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.source = self.root / "source"
        self.source.mkdir()
        self.manifest = self.root / "resume.json"

    def add_image(self, name="a.jpg", data=b"pixels"):
        path = self.source / name
        path.write_bytes(data)
        return path

    def run_import(self, fake, token=None):
        return importer.run_import(self.source, fake.url, self.manifest, token)

    def write_manifest(self, api_url, entries):
        self.manifest.write_text(json.dumps({
            "version": 1,
            "source_root": str(self.source.resolve()),
            "api_url": api_url,
            "files": entries,
        }))

    def test_upload_uses_snapshot_bytes_and_preserves_source_and_contract_metadata(self):
        data = b"unchanged image bytes\x00\xff"
        path = self.add_image("nested name.jpg", data)
        before = sha(path.read_bytes())
        state = FakeState()
        state.upload_hash = sha(data)
        with FakeKura(state) as fake:
            self.assertEqual(self.run_import(fake, "token-value"), 0)
        self.assertEqual(len(state.posts), 1)
        body = state.posts[0]
        self.assertIn(b'name="tags"\r\n\r\ntest-data', body)
        self.assertIn(b'name="tags"\r\n\r\nfolder-test', body)
        self.assertIn(b'name="source"\r\n\r\nfolder-test/nested name.jpg', body)
        self.assertIn(data, body)
        self.assertEqual(sha(path.read_bytes()), before)
        manifest = json.loads(self.manifest.read_text())
        entry = manifest["files"]["nested name.jpg"]
        self.assertEqual(entry["status"], "imported")
        self.assertEqual(entry["sha256"], before)
        self.assertEqual(entry["post_id"], "100")
        self.assertNotIn("token-value", self.manifest.read_text())
        by_path = {urlsplit(path).path: auth for method, path, auth in state.requests if method == "GET"}
        self.assertIsNone(by_path["/health"])
        self.assertEqual(by_path["/api/capabilities"], "Bearer token-value")
        self.assertIsNone(by_path["/api/posts"])
        self.assertEqual(state.requests[-1][2], "Bearer token-value")
        self.assertFalse(list(self.root.glob("kura-import-snapshots-*")))

    def test_scan_includes_webp_as_supported(self):
        webp = self.source / "photo.webp"
        webp.write_bytes(b"RIFF\x04\x00\x00\x00WEBP")
        self.add_image("a.jpg", b"pixels")
        video = self.source / "clip.mp4"
        video.write_bytes(b"video")
        candidates, _counts, unsupported = importer.scan(self.source)
        names = [path.name for path in candidates]
        self.assertIn("photo.webp", names)
        self.assertIn("a.jpg", names)
        self.assertIn("clip.mp4", names)
        self.assertNotIn("webp", unsupported)
        self.assertNotIn("video", unsupported)
        self.assertIn(".webp", importer.SUPPORTED)
        self.assertIn(".avif", importer.SUPPORTED)
        self.assertIn(".mp4", importer.SUPPORTED)
        self.assertIn(".webm", importer.SUPPORTED)

    def test_empty_terminal_post_page_allows_first_upload(self):
        path = self.add_image()
        state = FakeState()
        state.upload_hash = sha(path.read_bytes())
        with FakeKura(state) as fake:
            self.assertEqual(self.run_import(fake), 0)
        self.assertEqual(len(state.posts), 1)

    def test_locked_capability_stops_before_hash_walk_or_upload(self):
        self.add_image()
        state = FakeState()
        state.capabilities = {"writes_require_auth": True, "can_write": False, "actor": None}
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "locked for writes"):
                self.run_import(fake, "wrong-token")
        self.assertFalse(any(path.startswith("/api/posts") for _, path, _ in state.requests))
        self.assertEqual(state.posts, [])
        self.assertFalse(self.manifest.exists())

    def test_capability_redirect_is_not_followed_or_forwarded(self):
        self.add_image()
        state = FakeState()
        state.redirect_capabilities = True
        with FakeKura(state) as fake:
            with self.assertRaises(importer.ImportErrorSafe):
                self.run_import(fake, "secret-token")
        self.assertEqual(state.redirect_visits, 0)
        self.assertEqual(state.posts, [])
        self.assertNotIn(("GET", "/capture", "Bearer secret-token"), state.requests)

    def test_malformed_and_repeating_pagination_fail_before_any_upload(self):
        cases = [
            {"posts": "bad", "next_cursor": None},
            {"posts": [{"id": "01"}], "next_cursor": None},
            {"posts": [{"id": "1"}, {"id": "1"}], "next_cursor": None},
        ]
        for index, first in enumerate(cases):
            with self.subTest(index=index):
                self.add_image(f"{index}.jpg")
                state = FakeState()
                state.pages = [first]
                with FakeKura(state) as fake:
                    with self.assertRaises(importer.ImportErrorSafe):
                        self.run_import(fake)
                self.assertEqual(state.posts, [])
                if self.manifest.exists():
                    self.manifest.unlink()

        self.add_image("repeat.jpg")
        state = FakeState()
        state.pages = [( [{"id": "1"}], "cursor-a" )]
        state.cursor_map = {"cursor-a": ([{"id": "2"}], "cursor-a")}
        state.details = {"1": {"id": "1", "hash": ""}, "2": {"id": "2", "hash": ""}}
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "repeated a pagination cursor"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])

    def test_empty_terminal_later_page_and_legacy_empty_hash_are_accepted(self):
        state = FakeState()
        state.pages = [([{"id": "1"}], "next")]
        state.cursor_map = {"next": ([], None)}
        state.details = {"1": {"id": "1", "hash": ""}}
        with FakeKura(state) as fake:
            self.assertEqual(importer.all_existing_hashes(fake.url), set())

    def test_post_detail_must_match_requested_id_before_writes(self):
        self.add_image()
        state = FakeState()
        state.pages = [([{"id": "1"}], None)]
        state.details = {"1": {"id": "2", "hash": "sha256:" + ("a" * 64)}}
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "different or malformed post ID"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])

    def test_whitespace_only_cursor_fails_before_writes(self):
        self.add_image()
        state = FakeState()
        state.pages = [([{"id": "1"}], "   ")]
        state.details = {"1": {"id": "1", "hash": ""}}
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "invalid pagination cursor"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])

    def test_lost_post_response_reconciles_and_resume_never_posts_twice(self):
        path = self.add_image()
        state = FakeState()
        state.upload_hash = sha(path.read_bytes())
        state.post_mode = "lost"
        with FakeKura(state) as fake:
            first = self.run_import(fake)
            self.assertEqual(first, 0)
            self.assertEqual(len(state.posts), 1)
            second = self.run_import(fake)
        self.assertEqual(second, 0)
        self.assertEqual(len(state.posts), 1)
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest["files"]["a.jpg"]["status"], "confirmed-after-uncertain-response")

    def test_renamed_unresolved_entry_blocks_upload(self):
        data = b"possibly already uploaded"
        self.add_image("renamed.jpg", data)
        state = FakeState()
        with FakeKura(state) as fake:
            self.write_manifest(fake.url, {
                "original.jpg": {"status": "uncertain", "sha256": sha(data), "size": len(data)},
            })
            before = self.manifest.read_bytes()
            with self.assertRaisesRegex(importer.ImportErrorSafe, "absent from browse-visible posts"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])
        self.assertEqual(self.manifest.read_bytes(), before)

    def test_missing_unresolved_entry_blocks_success_without_candidates(self):
        state = FakeState()
        with FakeKura(state) as fake:
            self.write_manifest(fake.url, {
                "missing.jpg": {"status": "pending", "sha256": sha(b"not visible"), "size": 11},
            })
            with self.assertRaisesRegex(importer.ImportErrorSafe, "missing.jpg"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])
        self.assertEqual(json.loads(self.manifest.read_text())["files"]["missing.jpg"]["status"], "pending")

    def test_visible_renamed_unresolved_entry_reconciles_without_post(self):
        data = b"confirmed under a different filename"
        self.add_image("renamed.jpg", data)
        state = FakeState()
        state.pages = [([{"id": "42"}], None)]
        state.details = {"42": {"id": "42", "hash": "sha256:" + sha(data)}}
        with FakeKura(state) as fake:
            self.write_manifest(fake.url, {
                "original.jpg": {"status": "uncertain", "sha256": sha(data), "size": len(data)},
            })
            self.assertEqual(self.run_import(fake), 0)
        self.assertEqual(state.posts, [])
        files = json.loads(self.manifest.read_text())["files"]
        self.assertEqual(files["original.jpg"]["status"], "confirmed-after-resume")
        self.assertEqual(files["renamed.jpg"]["status"], "duplicate")

    def test_corrupt_resume_manifest_is_left_unchanged(self):
        self.add_image()
        corrupted = b'{broken manifest\n'
        self.manifest.write_bytes(corrupted)
        state = FakeState()
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "corrupt"):
                self.run_import(fake)
        self.assertEqual(self.manifest.read_bytes(), corrupted)
        self.assertEqual(state.posts, [])

    def test_source_replacement_after_snapshot_does_not_change_sent_bytes(self):
        original = b"stable snapshot"
        path = self.add_image(data=original)
        state = FakeState()
        state.upload_hash = sha(original)
        real_snapshot = importer.snapshot_source

        def snapshot_then_change(source, directory):
            result = real_snapshot(source, directory)
            source.write_bytes(b"replacement after snapshot")
            return result

        with FakeKura(state) as fake, mock.patch.object(importer, "snapshot_source", side_effect=snapshot_then_change):
            self.assertEqual(self.run_import(fake), 0)
        self.assertIn(original, state.posts[0])
        self.assertNotIn(b"replacement after snapshot", state.posts[0])
        self.assertNotEqual(sha(path.read_bytes()), sha(original))

    def test_growing_source_during_snapshot_is_rejected_without_upload(self):
        path = self.add_image(data=b"before")
        real_open = os.open

        def open_then_grow(candidate, flags, *args, **kwargs):
            fd = real_open(candidate, flags, *args, **kwargs)
            if Path(candidate) == path:
                with path.open("ab") as stream:
                    stream.write(b"growth")
            return fd

        with tempfile.TemporaryDirectory() as snapshots, mock.patch.object(importer.os, "open", side_effect=open_then_grow):
            with self.assertRaises(importer.SourceChanged):
                importer.snapshot_source(path, snapshots)
            self.assertEqual(list(Path(snapshots).iterdir()), [])

    def test_redirected_upload_is_ambiguous_and_not_followed(self):
        path = self.add_image()
        state = FakeState()
        state.post_mode = "redirect"
        state.upload_hash = sha(path.read_bytes())
        with FakeKura(state) as fake:
            result = self.run_import(fake, "candidate-token")
        self.assertEqual(result, 2)
        self.assertEqual(len(state.posts), 1)
        self.assertEqual(state.redirect_visits, 0)
        self.assertEqual(json.loads(self.manifest.read_text())["files"]["a.jpg"]["status"], "uncertain")

    def test_known_401_stops_later_uploads(self):
        self.add_image("a.jpg", b"a")
        self.add_image("b.jpg", b"b")
        state = FakeState()
        state.post_status = 401
        with FakeKura(state) as fake:
            self.assertEqual(self.run_import(fake, "invalid"), 1)
        self.assertEqual(len(state.posts), 1)
        self.assertEqual(json.loads(self.manifest.read_text())["files"]["a.jpg"]["status"], "failed")
        self.assertNotIn("b.jpg", json.loads(self.manifest.read_text())["files"])

    def test_invalid_api_url_and_unknown_capability_mode_fail_closed(self):
        self.add_image()
        state = FakeState()
        with FakeKura(state) as fake:
            for api_url in ("ftp://localhost", "http://user:pass@localhost", fake.url + "?token=x", fake.url + "?", fake.url + "#fragment"):
                with self.subTest(api_url=api_url), self.assertRaises(importer.ImportErrorSafe):
                    importer.run_import(self.source, api_url, self.manifest)
        state.capabilities = {"writes_require_auth": "maybe", "can_write": True, "actor": "local"}
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "unknown or malformed"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])

    def test_scan_error_fails_before_manifest_or_upload_writes(self):
        self.add_image()
        state = FakeState()
        with FakeKura(state) as fake, mock.patch.object(importer.os, "walk", side_effect=OSError("unreadable directory")):
            with self.assertRaisesRegex(OSError, "unreadable"):
                self.run_import(fake)
        self.assertEqual(state.posts, [])
        self.assertFalse(self.manifest.exists())

    def test_existing_manifest_lock_is_not_removed_or_overwritten(self):
        self.add_image()
        state = FakeState()
        lock = Path(str(self.manifest) + ".lock")
        lock.write_text("unknown owner\n")
        with FakeKura(state) as fake:
            with self.assertRaisesRegex(importer.ImportErrorSafe, "Another import"):
                self.run_import(fake)
        self.assertEqual(lock.read_text(), "unknown owner\n")
        self.assertEqual(state.posts, [])


if __name__ == "__main__":
    unittest.main()
