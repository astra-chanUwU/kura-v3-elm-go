"""Opt-in restore drill. Creates/drops only uniquely named disposable databases.

Set KURA_BACKUP_TEST_ADMIN_URL to a local PostgreSQL admin database URL.
Requires Python 3.9+, Go, psql, pg_dump, and pg_restore. No development library
or media is read. Each API instance and all its workers stop before backup.
"""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit, urlunsplit
from urllib.request import Request, urlopen
import zlib


REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts/library-backup.py"
SPEC = importlib.util.spec_from_file_location("library_backup_drill", SCRIPT)
BACKUP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BACKUP)


def png(color):
    width, height = 640, 400
    def chunk(kind, data):
        return struct.pack("!I", len(data)) + kind + data + struct.pack("!I", zlib.crc32(kind + data))
    rows = (b"\x00" + bytes(color) * width) * height
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack("!2I5B", width, height, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL to run the restore drill")
class RestoreDrill(unittest.TestCase):
    def setUp(self):
        parsed = urlsplit(os.environ["KURA_BACKUP_TEST_ADMIN_URL"])
        self.assertIn(parsed.hostname, {"127.0.0.1", "localhost", "::1"}, "drill is restricted to local PostgreSQL")
        self.admin_url = urlunsplit(parsed)
        self.prefix = "kura_backup_drill_" + secrets.token_hex(6)
        self.databases, self.servers = [], []
        self.temp = tempfile.TemporaryDirectory(prefix="kura-backup-acceptance-")
        self.root = Path(self.temp.name).resolve()
        self.addCleanup(self.cleanup)
        self.source = self.new_database("source")
        self.target = self.new_database("restored")
        self.media = self.root / "source-media"
        self.media.mkdir()
        self.binary = self.root / "kura-server"
        result = subprocess.run(["go", "-C", str(REPO / "server"), "build", "-o", str(self.binary), "./cmd/kura-server"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, "server build failed: " + result.stderr)
        for migration in sorted((REPO / "db/migrations").glob("*.sql")):
            self.pg(self.source, ["-f", str(migration)])

    def database_url(self, name):
        parts = urlsplit(self.admin_url)
        return urlunsplit((parts.scheme, parts.netloc, "/" + name, parts.query, ""))

    def environment(self, name):
        env = os.environ.copy()
        for key in ("S3_ENDPOINT", "S3_BUCKET", "AWS_S3_ENDPOINT", "AWS_S3_BUCKET", "S3_URL_PREFIX", "MEDIA_URL_PREFIX", "S3_PUBLIC_URL_PREFIX"):
            env.pop(key, None)
        env["DATABASE_URL"] = self.database_url(name)
        return env

    def pg(self, database, args):
        _, env = BACKUP.pg_environment(self.database_url(database))
        result = subprocess.run(["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", *args], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, "disposable PostgreSQL command failed")
        return result.stdout.strip()

    def new_database(self, suffix):
        name = self.prefix + "_" + suffix
        admin = urlsplit(self.admin_url).path.lstrip("/")
        self.pg(admin, ["-c", 'CREATE DATABASE "' + name + '"'])
        self.databases.append(name)
        return name

    def cleanup(self):
        for server in self.servers:
            self.stop_server(server)
        for name in reversed(self.databases):
            # Only names this test created are candidates for deletion.
            if name.startswith(self.prefix + "_"):
                _, env = BACKUP.pg_environment(self.admin_url)
                subprocess.run(["psql", "-X", "-v", "ON_ERROR_STOP=1", "-c", 'DROP DATABASE IF EXISTS "' + name + '" WITH (FORCE)'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.temp.cleanup()

    def start_server(self, database, media):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env = self.environment(database)
        env.update(HTTP_ADDR="127.0.0.1:" + str(port), MEDIA_ROOT=str(media), KURA_API_TOKEN="restore-drill-token")
        process = subprocess.Popen([str(self.binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.servers.append(process)
        base = "http://127.0.0.1:" + str(port)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                self.request(base, "GET", "/api/posts")
                return process, base
            except (URLError, HTTPError):
                time.sleep(0.05)
        self.fail("disposable Kura API did not become ready")

    @staticmethod
    def stop_server(process):
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)

    @staticmethod
    def request(base, method, path, body=None, content_type=None):
        if isinstance(body, dict):
            body, content_type = json.dumps(body).encode(), "application/json"
        headers = {"Authorization": "Bearer restore-drill-token"}
        if content_type:
            headers["Content-Type"] = content_type
        with urlopen(Request(base + path, data=body, headers=headers, method=method), timeout=5) as response:
            data = response.read()
            if path.startswith("/media/"):
                return data
            return json.loads(data)

    def upload(self, base, payload):
        boundary = "kura-drill-boundary"
        body = ("--" + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="drill.png"\r\nContent-Type: image/png\r\n\r\n').encode() + payload + ("\r\n--" + boundary + "--\r\n").encode()
        return self.request(base, "POST", "/api/uploads", body, "multipart/form-data; boundary=" + boundary)

    def wait_for_thumbnail(self, base, post_id):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            detail = self.request(base, "GET", "/api/posts/" + post_id)
            if detail.get("derivative_status") == "succeeded":
                return detail
            time.sleep(0.1)
        self.fail("thumbnail job did not complete in the disposable library")

    def snapshots(self, database):
        tables = self.pg(database, ["-c", "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name"]).splitlines()
        return {table: json.loads(self.pg(database, ["-c", 'SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),\'[]\'::jsonb) FROM "' + table + '" t'])) for table in tables}

    def tool(self, database, *args, success=True):
        result = subprocess.run([sys.executable, str(SCRIPT), *args], env=self.environment(database), capture_output=True, text=True)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, "unsafe operation unexpectedly succeeded")
        return result

    def test_full_restore_round_trip_and_guards(self):
        process, base = self.start_server(self.source, self.media)
        first = self.upload(base, png((12, 35, 90)))
        second = self.upload(base, png((99, 20, 75)))
        first_id, second_id = first["id"], second["id"]
        self.wait_for_thumbnail(base, first_id)
        hidden = self.wait_for_thumbnail(base, second_id)
        self.request(base, "POST", "/api/posts/tags", {"posts": [{"id": first_id, "version": 0}], "add": ["restore-drill"], "remove": []})
        self.request(base, "POST", "/api/posts/reactions", {"posts": [{"id": first_id, "version": 0}], "favorite": True, "score": 7})
        collection = self.request(base, "POST", "/api/collections", {"name": "Restore drill"})
        self.request(base, "POST", "/api/collections/" + collection["id"] + "/posts", {"post_ids": [second_id, first_id]})
        self.request(base, "POST", "/api/saved-searches", {"name": "Drill favorites", "query": "favorite:true"})
        self.request(base, "POST", "/api/posts/moderation", {"posts": [{"id": second_id}], "action": "hide", "reason": "restore drill"})
        self.stop_server(process)
        expected = self.snapshots(self.source)
        expected_media = {p.relative_to(self.media): hashlib.sha256(p.read_bytes()).hexdigest() for p in self.media.rglob("*") if p.is_file()}
        self.assertGreaterEqual(len(expected_media), 4, "both originals and thumbnails must be present")
        backup = self.root / "backup"
        restored_media = self.root / "restored-media"
        self.tool(self.source, "create", "--output", str(backup), "--media-root", str(self.media), "--offline")
        self.tool(self.source, "verify", "--input", str(backup))
        self.tool(self.target, "restore", "--input", str(backup), "--media-root", str(restored_media), "--offline")
        self.assertEqual(expected, self.snapshots(self.target), "all tables must restore exactly, including histories/auth/jobs")
        self.assertEqual(expected_media, {p.relative_to(restored_media): hashlib.sha256(p.read_bytes()).hexdigest() for p in restored_media.rglob("*") if p.is_file()})
        self.assertEqual(expected, self.snapshots(self.source), "backup must not change the source DB")
        manifest = (backup / "manifest.json").read_text()
        self.assertNotIn(urlsplit(self.admin_url).password or "unused-test-password-marker", manifest)
        self.tool(self.source, "create", "--output", str(backup), "--media-root", str(self.media), "--offline", success=False)
        blocked_media = self.root / "blocked-media"
        self.tool(self.target, "restore", "--input", str(backup), "--media-root", str(blocked_media), "--offline", success=False)
        self.assertFalse(blocked_media.exists())
        self.assertEqual(expected, self.snapshots(self.target))
        corrupted = self.root / "corrupted"
        shutil.copytree(backup, corrupted)
        relative = next(iter(expected_media))
        with (corrupted / "media" / relative).open("ab") as stream:
            stream.write(b"tamper")
        empty = self.new_database("empty")
        tamper_media = self.root / "tamper-media"
        self.tool(empty, "restore", "--input", str(corrupted), "--media-root", str(tamper_media), "--offline", success=False)
        self.assertEqual(self.snapshots(empty), {})
        self.assertFalse(tamper_media.exists())
        self.pg(empty, ["-c", "CREATE TYPE drill_existing_type AS ENUM ('occupied')"])
        self.tool(empty, "restore", "--input", str(backup), "--media-root", str(tamper_media), "--offline", success=False)
        self.assertFalse(tamper_media.exists())
        self.pg(empty, ["-c", "DROP TYPE drill_existing_type; CREATE SCHEMA pgcustom"])
        self.tool(empty, "restore", "--input", str(backup), "--media-root", str(tamper_media), "--offline", success=False)
        self.assertFalse(tamper_media.exists(), "a pg-prefixed user schema is still existing user state")

        # A valid manifest cannot make an invalid PostgreSQL archive restorable.
        invalid = self.root / "invalid-archive"
        shutil.copytree(backup, invalid)
        bad_dump = invalid / "database.dump"
        bad_dump.write_bytes(b"not a pg_dump archive")
        invalid_manifest = json.loads((invalid / "manifest.json").read_text())
        for entry in invalid_manifest["files"]:
            if entry["path"] == "database.dump":
                entry.update(size=bad_dump.stat().st_size, sha256=hashlib.sha256(bad_dump.read_bytes()).hexdigest())
        (invalid / "manifest.json").write_text(json.dumps(invalid_manifest))
        archive_db = self.new_database("badarchive")
        self.tool(archive_db, "restore", "--input", str(invalid), "--media-root", str(self.root / "badarchive-media"), "--offline", success=False)
        self.assertEqual(self.snapshots(archive_db), {})

        # Inject a conflicting object after the empty check. The real restore
        # must roll back its transaction and preserve the intervening object.
        failed_db = self.new_database("rollback")
        failed_media = self.root / "rollback-media"
        original_run_pg = BACKUP.run_pg
        def conflicting_restore(args, env, label, **kwargs):
            if label == "pg_restore":
                self.pg(failed_db, ["-c", "CREATE TABLE posts (id integer); INSERT INTO posts VALUES (912345)"])
            return original_run_pg(args, env, label, **kwargs)
        restore_args = type("RestoreArgs", (), {"offline": True, "input": str(backup), "media_root": str(failed_media)})()
        with mock.patch.dict(os.environ, self.environment(failed_db), clear=True), mock.patch.object(BACKUP, "run_pg", side_effect=conflicting_restore):
            with self.assertRaisesRegex(BACKUP.BackupError, "outcome could not be confirmed"):
                BACKUP.restore_backup(restore_args)
        self.assertEqual(self.snapshots(failed_db), {"posts": [{"id": 912345}]}, "partial restored tables must roll back")
        self.assertFalse(failed_media.exists())
        self.assertTrue(any((p / "media" / relative).is_file() for p in self.root.glob(".kura-restore-*")), "failed restore keeps recovery media")

        # After a real successful DB commit, late destination creation must
        # leave that destination untouched and retain the staged originals.
        publication_db = self.new_database("publication")
        collision_media = self.root / "collision-media"
        def collide(source, destination):
            destination.mkdir()
            (destination / "keep-me").write_text("concurrent destination")
            raise FileExistsError(destination)
        restore_args.media_root = str(collision_media)
        with mock.patch.dict(os.environ, self.environment(publication_db), clear=True), mock.patch.object(BACKUP, "exclusive_rename", side_effect=collide):
            with self.assertRaisesRegex(BACKUP.BackupError, "Database restore committed") as partial:
                BACKUP.restore_backup(restore_args)
        recovery_media = Path(str(partial.exception).rsplit(": ", 1)[1])
        self.assertEqual(self.snapshots(publication_db), expected)
        self.assertEqual((collision_media / "keep-me").read_text(), "concurrent destination")
        self.assertEqual(expected_media, {p.relative_to(recovery_media): hashlib.sha256(p.read_bytes()).hexdigest() for p in recovery_media.rglob("*") if p.is_file()})
        restored_process, restored_base = self.start_server(self.target, restored_media)
        detail = self.request(restored_base, "GET", "/api/posts/" + first_id)
        self.assertEqual(detail["tags"], ["restore-drill"])
        self.assertTrue(detail["favorite"])
        self.assertEqual(detail["score"], 7)
        self.assertEqual(self.request(restored_base, "GET", detail["original_url"]), png((12, 35, 90)))
        self.assertGreater(len(self.request(restored_base, "GET", detail["preview_url"])), 0)
        self.assertEqual(len(self.request(restored_base, "GET", "/api/saved-searches")["saved_searches"]), 1)
        with self.assertRaises(HTTPError) as denied:
            self.request(restored_base, "GET", hidden["original_url"])
        self.assertEqual(denied.exception.code, 404)
        added = self.upload(restored_base, png((20, 200, 45)))
        self.assertGreater(int(added["id"]), max(int(first_id), int(second_id)), "post identity sequence must continue")
        changed = self.request(restored_base, "POST", "/api/posts/tags", {"posts": [{"id": first_id, "version": detail["tag_version"]}], "add": ["restored-write"], "remove": []})
        self.assertEqual(changed["posts"][0]["version"], detail["tag_version"] + 1)
        self.stop_server(restored_process)


if __name__ == "__main__":
    unittest.main()
