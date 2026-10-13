"""Opt-in generated-image daily-library drill on unique disposable databases."""

import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen
import zipfile

SPEC = importlib.util.spec_from_file_location("daily_library_fixture", Path(__file__).with_name("test_library_backup_integration.py"))
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class DailyLibraryDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def request(self, method, path, body=None):
        return self.fixture.request(self.api, method, path, body)

    def export(self, path, status=200):
        try:
            with urlopen(self.api + path, timeout=15) as response:
                self.assertEqual(response.status, status)
                self.assertEqual(response.headers.get("Content-Type"), "application/zip")
                return response.read()
        except HTTPError as error:
            self.assertEqual(error.code, status)
            error.close()

    def cli(self, *args):
        env = os.environ.copy()
        env["KURA_API_TOKEN"] = "restore-drill-token"
        env["KURA_API_URL"] = self.api
        result = subprocess.run(["go", "-C", str(FIXTURE.REPO / "cli"), "run", "./cmd/kura", "collection", *args], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_import_organize_trash_restore_export_and_suggestions(self):
        payload = FIXTURE.png((39, 72, 101))
        post = self.fixture.upload(self.api, payload)
        post_id = post["id"]
        self.fixture.wait_for_thumbnail(self.api, post_id)
        self.request("POST", "/api/posts/tags", {"posts": [{"id": post_id, "version": 0}], "add": ["reference", "refined"], "remove": []})
        collection = self.request("POST", "/api/collections", {"name": "Daily drill"})
        collection_id = collection["id"]
        collection = self.request("POST", "/api/collections/" + collection_id + "/posts", {"post_ids": [post_id]})
        version = collection["version"]
        renamed = self.cli("rename", "--json", "--expected-version", str(version), collection_id, "References")
        self.assertEqual(renamed["version"], version + 1)
        version = renamed["version"]
        summaries = self.cli("summaries", "--json")["collections"]
        self.assertEqual(summaries[0]["visible_post_count"], 1)
        moved = self.cli("move", "--json", "--expected-version", str(version), "--end", collection_id, post_id)
        self.assertFalse(moved["changed"])
        self.assertEqual(moved["id"], collection_id)
        suggestions = self.request("GET", "/api/tags/suggestions?prefix=ref&limit=12")["tags"]
        self.assertEqual({row["tag"] for row in suggestions}, {"reference", "refined"})

        archive = self.export("/api/collections/" + collection_id + "/export?expected_version=" + str(version))
        with zipfile.ZipFile(io.BytesIO(archive)) as bundle:
            original = bundle.read("originals/" + post_id + ".png")
            self.assertEqual(original, payload)
            metadata = json.loads(bundle.read("metadata.json"))
            self.assertEqual(metadata["posts"][0]["hash"], "sha256:" + hashlib.sha256(payload).hexdigest())
            self.assertEqual(set(metadata["posts"][0]["tags"]), {"reference", "refined"})
        self.export("/api/collections/" + collection_id + "/export?expected_version=" + str(version - 1), 409)

        try:
            urlopen(self.api + "/api/posts/trash", timeout=5)
            self.fail("anonymous trash read succeeded")
        except HTTPError as error:
            self.assertEqual(error.code, 401)
            error.close()
        trashed = self.request("POST", "/api/posts/trash", {"post_ids": [post_id], "reason": "daily drill"})
        self.assertTrue(trashed["posts"][0]["changed"])
        rows = self.request("GET", "/api/posts/trash")["posts"]
        self.assertEqual(rows[0]["id"], post_id)
        self.assertEqual(self.request("GET", "/api/posts")["posts"], [])
        self.assertEqual(self.request("GET", "/api/tags/suggestions?prefix=ref")["tags"], [])
        self.export("/api/posts/export?ids=" + post_id, 404)
        summary = self.request("GET", "/api/collections/summaries")["collections"][0]
        self.assertEqual((summary["member_count"], summary["visible_post_count"]), (1, 0))
        # The protected metadata list never makes the original public again.
        self.export(post["original_url"], 404)
        self.request("POST", "/api/posts/restore", {"post_ids": [post_id]})
        self.assertEqual(self.request("GET", "/api/posts/" + post_id)["tags"], ["reference", "refined"])
        self.assertEqual(self.request("GET", post["original_url"]), payload)

        # An ordinary folder import previews without writes, attaches the exact
        # duplicate, uploads one new file, and resumes without making more posts.
        folder = self.fixture.root / "ordinary-import"
        folder.mkdir()
        (folder / "duplicate.png").write_bytes(payload)
        (folder / "new.png").write_bytes(FIXTURE.png((99, 44, 77)))
        manifest = self.fixture.root / "ordinary-import.json"
        env = os.environ.copy()
        env["KURA_API_TOKEN"] = "restore-drill-token"
        command = [sys.executable, str(FIXTURE.REPO / "scripts/import-media.py"), str(folder), "--api-url", self.api, "--manifest", str(manifest), "--tag", "ordinary", "--collection", collection_id]
        preview = subprocess.run(command + ["--dry-run"], env=env, capture_output=True, text=True)
        self.assertEqual(preview.returncode, 0, preview.stderr)
        self.assertFalse(manifest.exists())
        self.assertEqual(len(self.request("GET", "/api/posts")["posts"]), 1)
        for _ in range(2):
            imported = subprocess.run(command, env=env, capture_output=True, text=True)
            self.assertEqual(imported.returncode, 0, imported.stderr + imported.stdout)
        rows = self.request("GET", "/api/posts")["posts"]
        self.assertEqual(len(rows), 2)
        new = next(row for row in rows if row["id"] != post_id)
        self.assertEqual(new["tags"], ["ordinary"])
        self.assertEqual(self.request("GET", "/api/collections/summaries")["collections"][0]["member_count"], 2)

        # Collection deletion removes organization only, preserving media/posts.
        version = self.request("GET", "/api/collections/summaries")["collections"][0]["version"]
        self.cli("delete", "--json", "--expected-version", str(version), "--yes", collection_id)
        self.assertEqual(self.request("GET", "/api/collections/summaries")["collections"], [])
        self.assertEqual(len(self.request("GET", "/api/posts")["posts"]), 2)
        self.assertEqual(self.request("GET", post["original_url"]), payload)


if __name__ == "__main__":
    unittest.main()
