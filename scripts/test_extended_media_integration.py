"""Opt-in acceptance of AVIF, animated WebP, and bounded video uploads.

Set KURA_EXTENDED_INTEGRATION_TESTS=1 and KURA_BACKUP_TEST_ADMIN_URL to a
local disposable PostgreSQL admin URL. The drill creates and drops only
uniquely named databases. It requires the installed ffmpeg and ffprobe tools;
it never installs or downloads them.
"""

import hashlib
import io
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import urlopen
import zipfile

import test_library_backup_integration as FIXTURE


FORMATS = (
    ("static.avif", "image/avif", ".avif", 32, 24, True),
    ("animated.webp", "image/webp", ".webp", 32, 24, True),
    ("h264-aac.mp4", "video/mp4", ".mp4", 32, 24, False),
    ("vp9-opus.webm", "video/webm", ".webm", 32, 24, False),
)


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class ExtendedMediaDrill(unittest.TestCase):
    def setUp(self):
        if os.environ.get("KURA_EXTENDED_INTEGRATION_TESTS") != "1":
            self.skipTest("set KURA_EXTENDED_INTEGRATION_TESTS=1 to exercise installed ffmpeg tools")
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        self.api_process, self.api = self.start_server(self.fixture.source, self.fixture.media)

    def start_server(self, database, media, missing_tools=False):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env = self.fixture.environment(database)
        env.update(HTTP_ADDR="127.0.0.1:" + str(port), MEDIA_ROOT=str(media), KURA_API_TOKEN="restore-drill-token")
        env.pop("KURA_FFPROBE_PATH", None)
        env.pop("KURA_FFMPEG_PATH", None)
        if missing_tools:
            env["KURA_FFPROBE_PATH"] = str(self.fixture.root / "missing-ffprobe")
            env["KURA_FFMPEG_PATH"] = str(self.fixture.root / "missing-ffmpeg")
        else:
            if shutil.which("ffprobe"):
                env["KURA_FFPROBE_PATH"] = shutil.which("ffprobe")
            if shutil.which("ffmpeg"):
                env["KURA_FFMPEG_PATH"] = shutil.which("ffmpeg")
        process = subprocess.Popen([str(self.fixture.binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.fixture.servers.append(process)
        base = "http://127.0.0.1:" + str(port)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                self.fixture.request(base, "GET", "/api/posts")
                return process, base
            except (URLError, HTTPError):
                time.sleep(0.05)
        self.fail("disposable Kura API did not become ready")

    def extended_fixture(self, name):
        return (FIXTURE.REPO / "server" / "internal" / "media" / "testdata" / "extended" / name).read_bytes()

    def test_extended_uploads_keep_originals_and_make_jpeg_posters(self):
        if shutil.which("ffmpeg") is None or shutil.which("ffprobe") is None:
            self.skipTest("extended media acceptance requires installed ffmpeg and ffprobe; no tools were installed")
        originals = {}
        video_ids = []
        for filename, media_type, extension, width, height, image_indexable in FORMATS:
            payload = self.extended_fixture(filename)
            post = self.fixture.upload(self.api, payload)
            originals[post["id"]] = (payload, extension)
            self.assertEqual((post["media_type"], post["width"], post["height"]), (media_type, width, height), filename)
            self.assertTrue(post["original_url"].endswith(extension), (filename, post["original_url"]))
            self.assertEqual(post["hash"], "sha256:" + hashlib.sha256(payload).hexdigest(), filename)
            if not image_indexable:
                video_ids.append(post["id"])

            detail = self.fixture.wait_for_thumbnail(self.api, post["id"])
            preview = self.fixture.request(self.api, "GET", detail["preview_url"])
            self.assertTrue(preview.startswith(b"\xff\xd8"), filename + " poster must be JPEG")
            self.assertEqual(self.fixture.request(self.api, "GET", post["original_url"]), payload, filename + " original changed")

        with urlopen(self.api + "/api/posts/export?ids=" + ",".join(originals) + "&metadata=true", timeout=10) as response:
            with zipfile.ZipFile(io.BytesIO(response.read())) as bundle:
                for post_id, (payload, extension) in originals.items():
                    self.assertEqual(bundle.read("originals/" + post_id + extension), payload)

        # Video stays browseable as media but is ineligible for the image-only
        # similarity index and never receives a fingerprint job.
        status = self.fixture.request(self.api, "GET", "/api/posts/similarity/status")
        visible_index_ids = {row["id"] for row in status["posts"]}
        self.assertTrue(set(originals).difference(video_ids).issubset(visible_index_ids))
        self.assertTrue(set(video_ids).isdisjoint(visible_index_ids))
        for video_id in video_ids:
            with self.assertRaises(HTTPError) as error:
                self.fixture.request(self.api, "POST", "/api/posts/similarity/index", {"post_ids": [video_id]})
            self.assertEqual(error.exception.code, 404)
            error.exception.close()
        query = "SELECT count(*) FROM jobs j JOIN posts p ON p.id::text = j.payload->>'post_id' WHERE j.kind = 'fingerprint' AND p.media_type LIKE 'video/%'"
        self.assertEqual(self.fixture.pg(self.fixture.source, ["-c", query]), "0")

    def test_missing_ffmpeg_tools_reject_video_without_database_rows(self):
        self.fixture.stop_server(self.api_process)
        self.api_process, self.api = self.start_server(self.fixture.source, self.fixture.media, missing_tools=True)
        before = self.fixture.pg(self.fixture.source, ["-c", "SELECT (SELECT count(*) FROM posts), (SELECT count(*) FROM jobs)"])
        with self.assertRaises(HTTPError) as error:
            self.fixture.upload(self.api, self.extended_fixture("h264-aac.mp4"))
        self.assertEqual(error.exception.code, 400)
        error.exception.close()
        after = self.fixture.pg(self.fixture.source, ["-c", "SELECT (SELECT count(*) FROM posts), (SELECT count(*) FROM jobs)"])
        self.assertEqual(after, before, "missing ffprobe must not create post or job rows")
        self.assertFalse(list(self.fixture.media.rglob(".upload-*")), "failed validation left upload staging files")


if __name__ == "__main__":
    unittest.main()
