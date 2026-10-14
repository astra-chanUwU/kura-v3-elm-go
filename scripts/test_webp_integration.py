"""Static WebP uploads, derivatives, immutable exports and folder import."""
import base64
import hashlib
import io
import json
import os
import subprocess
import sys
import unittest
import zipfile
from urllib.error import HTTPError
from urllib.request import urlopen

import test_library_backup_integration as FIXTURE

SAMPLES = {'import-new.webp': 'UklGRh4AAABXRUJQVlA4TBEAAAAvF8AEAAdQvYJ1s/+BiOh/AAA=', 'lossless-alpha.webp': 'UklGRh4AAABXRUJQVlA4TBEAAAAvD8ACEAfQlo41r1qBiOh/AAA=', 'lossy.webp': 'UklGRkAAAABXRUJQVlA4IDQAAADwAgCdASogABgAPlEkj0WjoiEUBAA4BQSzgDsAAEBQUAD+9Ykf/xQCkcOd//Qo2US+YAAA', 'animated.webp': 'UklGRrYAAABXRUJQVlA4WAoAAAACAAAACwAABwAAQU5JTQYAAAAAAAAAAABBTk1GQAAAAAAAAAAAAAsAAAcAAGQAAAJWUDggKAAAADABAJ0BKgwACAABQCYlmAADcAD+8ql//4c+2P3sY///2L9gfxLGgABBTk1GQgAAAAAAAAAAAAsAAAcAAGQAAABWUDggKgAAAFQBAJ0BKgwACAAAACYlmAAEYYAA/vaVl//8EF/EF/EFhq/+LuxHGXQAAA=='}

@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class WebPDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def test_static_webp_derivatives_exports_rejection_and_import(self):
        originals = {}
        for name, size in (("lossy.webp", (32, 24)), ("lossless-alpha.webp", (16, 12))):
            payload = base64.b64decode(SAMPLES[name])
            # Deliberately claims PNG; decoded content determines the format.
            post = self.fixture.upload(self.api, payload)
            originals[post["id"]] = payload
            self.assertEqual((post["media_type"], post["width"], post["height"]), ("image/webp", *size))
            self.assertTrue(post["original_url"].endswith(".webp"))
            ready = self.fixture.wait_for_thumbnail(self.api, post["id"])
            preview = self.fixture.request(self.api, "GET", ready["preview_url"])
            self.assertTrue(preview.startswith(b"\xff\xd8"), "WebP thumbnail must use the existing JPEG format")
            self.assertEqual(self.fixture.request(self.api, "GET", post["original_url"]), payload)
            self.assertEqual(post["hash"], "sha256:" + hashlib.sha256(payload).hexdigest())
        with urlopen(self.api + "/api/posts/export?ids=" + ",".join(originals) + "&metadata=true", timeout=10) as response:
            with zipfile.ZipFile(io.BytesIO(response.read())) as bundle:
                for post_id, payload in originals.items():
                    self.assertEqual(bundle.read("originals/" + post_id + ".webp"), payload)
        before = self.fixture.pg(self.fixture.source, ["-c", "SELECT (SELECT count(*) FROM posts), (SELECT count(*) FROM jobs)"])
        lossy = base64.b64decode(SAMPLES["lossy.webp"])
        for payload in (base64.b64decode(SAMPLES["animated.webp"]), lossy[:-4], lossy[:24], b"RIFF\xff\xff\xff\xffWEBPVP8 junk"):
            with self.assertRaises(HTTPError) as error:
                self.fixture.upload(self.api, payload)
            self.assertEqual(error.exception.code, 400)
            error.exception.close()
        after = self.fixture.pg(self.fixture.source, ["-c", "SELECT (SELECT count(*) FROM posts), (SELECT count(*) FROM jobs)"])
        self.assertEqual(after, before)
        self.assertFalse(list(self.fixture.media.rglob(".upload-*")))
        folder = self.fixture.root / "webp-import"
        folder.mkdir()
        (folder / "copy.webp").write_bytes(lossy)
        (folder / "new.webp").write_bytes(base64.b64decode(SAMPLES["import-new.webp"]))
        env = os.environ.copy();env["KURA_API_TOKEN"] = "restore-drill-token"
        manifest = self.fixture.root / "webp-resume.json"
        command = [sys.executable, str(FIXTURE.REPO / "scripts/import-media.py"), str(folder), "--api-url", self.api, "--manifest", str(manifest)]
        preview = subprocess.run(command + ["--dry-run"], env=env, capture_output=True, text=True)
        self.assertEqual(preview.returncode, 0, preview.stderr)
        self.assertIn("duplicate (skip): copy.webp", preview.stdout)
        self.assertFalse(manifest.exists())
        for _ in range(2):
            imported = subprocess.run(command, env=env, capture_output=True, text=True)
            self.assertEqual(imported.returncode, 0, imported.stdout + imported.stderr)
        rows = self.fixture.request(self.api, "GET", "/api/posts")["posts"]
        self.assertEqual(len(rows), 3)
        imported_post = next(row for row in rows if row["id"] not in originals)
        self.assertEqual(imported_post["media_type"], "image/webp")
        detail = self.fixture.request(self.api, "GET", "/api/posts/" + imported_post["id"])
        self.assertEqual(self.fixture.request(self.api, "GET", detail["original_url"]), base64.b64decode(SAMPLES["import-new.webp"]))

if __name__ == "__main__":
    unittest.main()
