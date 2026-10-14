"""Disposable HTTP integration drill for browser folder imports and metadata."""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import unittest
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


HELPER = Path(__file__).with_name("test_library_backup_integration.py")
SPEC = importlib.util.spec_from_file_location("browser_import_api_fixture", HELPER)
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class BrowserImportDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def call(self, method, path, body=None, token="restore-drill-token", content_type=None):
        if isinstance(body, dict):
            body, content_type = json.dumps(body).encode(), "application/json"
        headers = {"Authorization": "Bearer " + token}
        if content_type:
            headers["Content-Type"] = content_type
        try:
            with urlopen(Request(self.api + path, data=body, headers=headers, method=method), timeout=8) as response:
                data = response.read()
                return response.status, json.loads(data) if data else None
        except HTTPError as error:
            data = error.read()
            return error.code, json.loads(data) if data else None

    def multipart(self, payload, filename, fields, token="restore-drill-token"):
        boundary = "kura-browser-import-acceptance-boundary"
        chunks = []
        for name, value in fields:
            chunks.extend([
                ("--" + boundary + "\r\n").encode(),
                ('Content-Disposition: form-data; name="' + name + '"\r\n\r\n').encode(),
                str(value).encode(),
                b"\r\n",
            ])
        chunks.extend([
            ("--" + boundary + "\r\n").encode(),
            ('Content-Disposition: form-data; name="file"; filename="' + filename + '"\r\n').encode(),
            b"Content-Type: image/png\r\n\r\n",
            payload,
            b"\r\n--" + boundary.encode() + b"--\r\n",
        ])
        return self.call("POST", "/api/imports", b"".join(chunks), token, "multipart/form-data; boundary=" + boundary)

    def upload_fields(self, payload, **overrides):
        fields = [
            ("expected_hash", overrides.get("expected_hash", "sha256:" + hashlib.sha256(payload).hexdigest())),
            ("tags", overrides.get("tags", "browser-test, generated")),
            ("source", overrides.get("source", "generated/original.png")),
            ("artist", overrides.get("artist", "Fixture Artist")),
            ("duplicate_policy", overrides.get("duplicate_policy", "skip")),
        ]
        if "collection_id" in overrides:
            fields.append(("collection_id", overrides["collection_id"]))
        return fields

    def durable_state(self):
        sql = (
            "SELECT (SELECT count(*) FROM posts)||':'||"
            "(SELECT count(*) FROM jobs)||':'||"
            "(SELECT count(*) FROM collection_posts)||':'||"
            "(SELECT COALESCE(string_agg(id::text||'='||version::text,',' ORDER BY id), '') FROM collections)"
        )
        database = self.fixture.pg(self.fixture.source, ["-c", sql])
        media = tuple(sorted((str(path.relative_to(self.fixture.media)), hashlib.sha256(path.read_bytes()).hexdigest())
                             for path in self.fixture.media.rglob("*") if path.is_file()))
        return database, media

    def import_file(self, payload, filename, **overrides):
        token = overrides.pop("token", "restore-drill-token")
        return self.multipart(payload, filename, self.upload_fields(payload, **overrides), token=token)

    def test_import_duplicate_metadata_and_authentication_contract(self):
        public_bytes = FIXTURE.png((19, 83, 141))
        private_bytes = FIXTURE.png((144, 61, 37))
        public_hash = "sha256:" + hashlib.sha256(public_bytes).hexdigest()
        private_hash = "sha256:" + hashlib.sha256(private_bytes).hexdigest()

        status, created = self.import_file(public_bytes, "public.png", tags="original, preserved", source="original/source", artist="Original Artist")
        self.assertEqual(status, 201, created)
        public_id = created["post"]["id"]
        self.fixture.wait_for_thumbnail(self.api, public_id)

        status, private = self.import_file(private_bytes, "private.png")
        self.assertEqual(status, 201, private)
        private_id = private["post"]["id"]
        self.fixture.wait_for_thumbnail(self.api, private_id)
        status, hidden = self.call("POST", "/api/posts/moderation", {
            "posts": [{"id": private_id}], "action": "hide", "reason": "browser import visibility drill",
        })
        self.assertEqual(status, 200, hidden)

        before = self.durable_state()
        status, preview = self.call("GET", "/api/imports/duplicates?" + urlencode({"hashes": ",".join([public_hash, private_hash])}))
        self.assertEqual(status, 200, preview)
        self.assertEqual(preview, {"matches": [{"hash": public_hash, "post_id": public_id}]})
        self.assertEqual(self.durable_state(), before, "duplicate preview must not write rows or media")

        collection_status, collection = self.call("POST", "/api/collections", {"name": "Browser import acceptance"})
        self.assertEqual(collection_status, 201, collection)
        collection_id = collection["id"]
        new_bytes = FIXTURE.png((34, 176, 92))
        new_hash = "sha256:" + hashlib.sha256(new_bytes).hexdigest()
        status, imported = self.import_file(
            new_bytes, "new-image.png", collection_id=collection_id,
            tags="collection-test, new-image", source="folder/new-image.png", artist="New Artist",
        )
        self.assertEqual(status, 201, imported)
        result = imported
        post = result["post"]
        post_id = post["id"]
        self.assertFalse(result["duplicate"])
        self.assertTrue(result["attached"])
        self.assertEqual(result["collection_id"], collection_id)
        self.assertEqual((post["hash"], post["source"], post["artist"]), (new_hash, "folder/new-image.png", "New Artist"))
        self.assertEqual(set(post["tags"]), {"collection-test", "new-image"})
        self.assertTrue(post["derivative_job_id"], "import must enqueue the durable derivative job")
        self.assertEqual(post["metadata_version"], 0)
        self.assertEqual(post["metadata_history"], [])

        status, collection_page = self.call("GET", "/api/collections/" + collection_id + "/posts")
        self.assertEqual(status, 200, collection_page)
        self.assertEqual([member["id"] for member in collection_page["posts"]], [post_id])
        membership_version = collection_page["collection_version"]
        status, job = self.call("GET", "/api/jobs/" + post["derivative_job_id"])
        self.assertEqual(status, 200, job)
        self.assertEqual(job["id"], post["derivative_job_id"])
        self.fixture.wait_for_thumbnail(self.api, post_id)

        before = self.durable_state()
        status, repeated = self.import_file(
            new_bytes, "second-name.png", collection_id=collection_id,
            tags="overwritten", source="should/not/replace", artist="Wrong Artist",
        )
        self.assertEqual(status, 200, repeated)
        self.assertTrue(repeated["duplicate"])
        self.assertEqual(repeated["post"]["id"], post_id)
        self.assertEqual((repeated["post"]["source"], repeated["post"]["artist"]), ("folder/new-image.png", "New Artist"))
        self.assertEqual(set(repeated["post"]["tags"]), {"collection-test", "new-image"})
        status, collection_page = self.call("GET", "/api/collections/" + collection_id + "/posts")
        self.assertEqual(status, 200, collection_page)
        self.assertEqual(collection_page["collection_version"], membership_version, "idempotent duplicate attachment must not bump collection version")
        self.assertEqual([member["id"] for member in collection_page["posts"]], [post_id])
        self.assertEqual(self.durable_state(), before, "repeated skip must not rewrite rows, jobs, membership, or media")

        before = self.durable_state()
        status, conflict = self.import_file(new_bytes, "duplicate-error.png", collection_id=collection_id, duplicate_policy="error")
        self.assertEqual(status, 409, conflict)
        self.assertEqual(self.durable_state(), before, "duplicate:error must have no durable effects")

        before = self.durable_state()
        status, mismatch = self.import_file(new_bytes, "changed-hash.png", collection_id=collection_id,
                                            expected_hash="sha256:" + "0" * 64)
        self.assertEqual(status, 400, mismatch)
        self.assertEqual(self.durable_state(), before, "changed expected_hash must be rejected before writes")

        status, detail = self.call("GET", "/api/posts/" + post_id)
        self.assertEqual(status, 200, detail)
        self.assertEqual(detail["metadata_version"], 0)
        self.assertEqual(detail["metadata_history"], [])
        update = {"posts": [{"id": post_id, "version": 0}], "source": "edited/source.png", "artist": "Edited Artist"}
        status, updated = self.call("POST", "/api/posts/metadata", update)
        self.assertEqual(status, 200, updated)
        self.assertEqual(updated["posts"][0]["version"], 1)
        self.assertTrue(updated["posts"][0]["changed"])
        status, detail = self.call("GET", "/api/posts/" + post_id)
        self.assertEqual(status, 200, detail)
        self.assertEqual((detail["source"], detail["artist"], detail["metadata_version"]), ("edited/source.png", "Edited Artist", 1))
        self.assertEqual(len(detail["metadata_history"]), 1)
        self.assertEqual(detail["metadata_history"][0]["previous_source"], "folder/new-image.png")

        status, noop = self.call("POST", "/api/posts/metadata", {
            "posts": [{"id": post_id, "version": 1}], "source": "edited/source.png", "artist": "Edited Artist",
        })
        self.assertEqual(status, 200, noop)
        self.assertEqual(noop["posts"][0]["version"], 1)
        self.assertFalse(noop["posts"][0]["changed"])
        status, stale = self.call("POST", "/api/posts/metadata", {
            "posts": [{"id": post_id, "version": 0}], "source": "stale/source.png",
        })
        self.assertEqual(status, 409, stale)
        status, detail_after_stale = self.call("GET", "/api/posts/" + post_id)
        self.assertEqual(status, 200, detail_after_stale)
        self.assertEqual(detail_after_stale["metadata_version"], 1)
        self.assertEqual(len(detail_after_stale["metadata_history"]), 1)
        self.assertEqual(detail_after_stale["source"], "edited/source.png")

        before = self.durable_state()
        status, denied = self.import_file(
            FIXTURE.png((211, 101, 13)), "wrong-token.png", collection_id=collection_id, token="wrong-token",
        )
        self.assertEqual(status, 401, denied)
        self.assertEqual(self.durable_state(), before, "wrong token must leave database and media untouched")


if __name__ == "__main__":
    unittest.main()
