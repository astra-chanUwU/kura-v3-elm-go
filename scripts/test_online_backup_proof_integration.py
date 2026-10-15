"""Opt-in proof that a PostgreSQL snapshot matches referenced local media.

Set KURA_BACKUP_TEST_ADMIN_URL to a local disposable PostgreSQL URL. Requires
psql, pg_dump, pg_restore, and Go. Creates and drops only uniquely named
databases and uses generated files in a temporary media directory.
"""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import zlib
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit, urlunsplit
from urllib.request import Request, urlopen


REPO = Path(__file__).resolve().parent.parent
PROOF_PATH = Path(__file__).with_name("online_backup_proof.py")
SPEC = importlib.util.spec_from_file_location("online_backup_proof_integration_subject", PROOF_PATH)
proof = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(proof)
backup = proof.backup

TOKEN = "online-backup-proof-token"
NAME_RE = re.compile(r"^[a-z0-9_]+$")


def png(n, width=16, height=16):
    color = (n % 256, (n * 3) % 256, (n * 7) % 256)

    def chunk(kind, data):
        return struct.pack("!I", len(data)) + kind + data + struct.pack("!I", zlib.crc32(kind + data))

    rows = (b"\x00" + bytes(color) * width) * height
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack("!2I5B", width, height, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))


@unittest.skipUnless(
    os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"),
    "set an explicitly disposable local admin URL to run the online backup proof",
)
class OnlineBackupSnapshotDrill(unittest.TestCase):
    databases = []

    @classmethod
    def setUpClass(cls):
        parsed = urlsplit(os.environ["KURA_BACKUP_TEST_ADMIN_URL"])
        if parsed.hostname not in {"127.0.0.1", "localhost", "::1"}:
            raise AssertionError("online backup proof is restricted to local PostgreSQL")
        for tool in ("psql", "pg_dump", "pg_restore"):
            if shutil.which(tool) is None:
                raise AssertionError(f"required PostgreSQL tool is missing: {tool}")
        cls.admin_url = urlunsplit(parsed)
        cls.prefix = "kura_obproof_" + secrets.token_hex(4)
        cls.databases = []
        cls.class_temp = tempfile.TemporaryDirectory(prefix="kura-online-backup-proof-")
        cls.addClassCleanup(cls.class_temp.cleanup)
        cls.addClassCleanup(cls.drop_proof_databases)
        cls.template = cls.create_database("template", template=None)
        for migration in sorted((REPO / "db" / "migrations").glob("*.sql")):
            cls.psql(cls.template, ["-f", str(migration)])

    @classmethod
    def quote_name(cls, name):
        if not name.startswith(cls.prefix + "_") or not NAME_RE.fullmatch(name):
            raise AssertionError("refusing to touch a database outside this proof")
        return '"' + name + '"'

    @classmethod
    def admin_env(cls):
        _database, env = backup.pg_environment(cls.admin_url)
        env.pop("KURA_BACKUP_TEST_ADMIN_URL", None)
        return env

    @classmethod
    def database_url(cls, name):
        cls.quote_name(name)
        parts = urlsplit(cls.admin_url)
        return urlunsplit((parts.scheme, parts.netloc, "/" + name, parts.query, ""))

    @classmethod
    def proof_env(cls, name):
        _database, env = backup.pg_environment(cls.database_url(name))
        env.pop("KURA_BACKUP_TEST_ADMIN_URL", None)
        for key in ("S3_ENDPOINT", "AWS_S3_ENDPOINT", "S3_BUCKET", "AWS_S3_BUCKET",
                    "S3_URL_PREFIX", "MEDIA_URL_PREFIX", "S3_PUBLIC_URL_PREFIX"):
            env.pop(key, None)
        return env

    @classmethod
    def psql(cls, database, args):
        result = subprocess.run(
            ["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", *args],
            env=cls.proof_env(database), capture_output=True, text=True,
        )
        if result.returncode != 0:
            raise AssertionError("disposable PostgreSQL command failed: " + proof.redact(result.stderr))
        return result.stdout.strip()

    @classmethod
    def admin_sql(cls, sql):
        admin = urlsplit(cls.admin_url).path.lstrip("/") or "postgres"
        _database, env = backup.pg_environment(cls.admin_url)
        env.pop("KURA_BACKUP_TEST_ADMIN_URL", None)
        env["PGDATABASE"] = admin
        result = subprocess.run(
            ["psql", "-X", "-v", "ON_ERROR_STOP=1", "-c", sql],
            env=env, capture_output=True, text=True,
        )
        if result.returncode != 0:
            raise AssertionError("disposable database maintenance failed: " + proof.redact(result.stderr))

    @classmethod
    def create_database(cls, suffix, template):
        name = cls.prefix + "_" + suffix
        cls.quote_name(name)
        if template is None:
            cls.admin_sql("CREATE DATABASE " + cls.quote_name(name))
        else:
            cls.quote_name(template)
            cls.admin_sql(
                "CREATE DATABASE " + cls.quote_name(name) + " TEMPLATE " + cls.quote_name(template)
            )
        cls.databases.append(name)
        return name

    @classmethod
    def drop_proof_databases(cls):
        for name in reversed(list(cls.databases)):
            if name.startswith(cls.prefix + "_"):
                cls.admin_sql("DROP DATABASE IF EXISTS " + cls.quote_name(name) + " WITH (FORCE)")
        cls.databases.clear()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="kura-online-backup-case-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.procs = []
        self.logs = []
        self.addCleanup(self.stop_procs)

    def stop_procs(self):
        for proc in self.procs:
            if proc.stdin is not None:
                proc.stdin.close()
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
        for handle in self.logs:
            handle.close()
        self.logs.clear()

    def clone(self, suffix):
        return self.create_database(suffix + secrets.token_hex(2), self.template)

    def seed_original(self, database, media, key, data, *, hidden=False, trashed=False):
        proof.link_immutable(media, key, data)
        digest = hashlib.sha256(data).hexdigest()
        state = "hidden" if hidden else "published"
        deleted = "now()" if trashed else "NULL"
        url = "/media/" + key
        row_id = self.psql(database, ["-c", (
            "INSERT INTO posts (preview_url, original_url, media_type, width, height, hash, file_size, moderation_state, deleted_at) "
            f"VALUES ('{url}', '{url}', 'image/png', 16, 16, 'sha256:{digest}', {len(data)}, '{state}', {deleted}) "
            "RETURNING id::text"
        )])
        self.psql(database, ["-c", (
            "INSERT INTO jobs (kind, payload, idempotency_key) VALUES ("
            "'derivative', jsonb_build_object("
            f"'post_id', '{row_id}', 'original_key', '{key}', 'variant', 'thumb-320'), "
            f"'derivative:{row_id}:thumb-320')"
        )])
        return row_id

    def json_query(self, database, sql):
        text = self.psql(database, ["-c", sql])
        return json.loads(text or "null")

    def reference_document(self, database):
        return self.json_query(database, proof.REFERENCE_SQL)

    def run_backup(self, database, media, output, **kwargs):
        kwargs.setdefault("allow_platform_rename", True)
        return proof.run_online_backup(media, output, proof.open_postgres_snapshot(self.proof_env(database)), **kwargs)

    def restore_dump(self, dump_path):
        target = self.create_database("restored" + secrets.token_hex(2), None)
        result = subprocess.run(
            ["pg_restore", "--single-transaction", "--exit-on-error", "--no-owner", "--no-acl",
             "--dbname", target, str(dump_path)],
            env=self.proof_env(target), capture_output=True,
        )
        if result.returncode != 0:
            self.fail("disposable restore failed: " + proof.redact(result.stderr.decode("utf-8", "replace")))
        return target

    def test_missing_corrupt_dump_and_interrupt_leave_no_backup(self):
        database = self.clone("guard")
        media = self.root / "media"
        media.mkdir()
        present = self.seed_original(database, media, "uploads/present.png", png(1))
        missing_key = "uploads/missing.png"
        self.seed_original(database, media, missing_key, png(2))
        (media / "uploads" / "missing.png").unlink()
        with self.assertRaisesRegex(proof.ProofError, "missing referenced media"):
            self.run_backup(database, media, self.root / "missing-backup")
        self.assertFalse((self.root / "missing-backup").exists())
        self.assertEqual(list(self.root.glob(proof.STAGING_PREFIX + "*")), [])

        (media / "uploads" / "present.png").write_bytes(b"tampered-bytes")
        # The missing key still fails first. Remove that post's file reference by restoring
        # a fresh library for the remaining failure modes.
        fresh = self.clone("fresh")
        fresh_media = self.root / "fresh-media"
        fresh_media.mkdir()
        self.seed_original(fresh, fresh_media, "uploads/original.png", png(3))
        (fresh_media / "uploads" / "original.png").write_bytes(b"corrupt-original")
        with self.assertRaisesRegex(proof.ProofError, "does not match the snapshot"):
            self.run_backup(fresh, fresh_media, self.root / "corrupt-backup")
        self.assertFalse((self.root / "corrupt-backup").exists())

        dumped = self.clone("dumped")
        dumped_media = self.root / "dumped-media"
        dumped_media.mkdir()
        self.seed_original(dumped, dumped_media, "uploads/dump.png", png(4))

        def break_dump(hold):
            hold.snapshot_id = "00000000-00000000-1"

        with self.assertRaisesRegex(proof.ProofError, "pg_dump failed"):
            self.run_backup(dumped, dumped_media, self.root / "dump-backup", before_dump=break_dump)
        self.assertFalse((self.root / "dump-backup").exists())
        self.assertEqual(list(self.root.glob(proof.STAGING_PREFIX + "*")), [])

        interrupted = self.clone("cut")
        cut_media = self.root / "cut-media"
        cut_media.mkdir()
        self.seed_original(interrupted, cut_media, "uploads/cut.png", png(5))

        def interrupt():
            raise proof.ProofError("backup interrupted")

        with self.assertRaisesRegex(proof.ProofError, "backup interrupted"):
            self.run_backup(interrupted, cut_media, self.root / "cut-backup", before_copy=interrupt)
        self.assertFalse((self.root / "cut-backup").exists())
        self.assertEqual(list(self.root.glob(proof.STAGING_PREFIX + "*")), [])
        self.assertTrue(present)

    def test_second_snapshot_is_rejected_while_the_first_is_open(self):
        database = self.clone("busy")
        media = self.root / "media"
        media.mkdir()
        self.seed_original(database, media, "uploads/one.png", png(6))
        errors = []

        def after_ready(_ready):
            def second():
                try:
                    self.run_backup(database, media, self.root / "second-backup")
                except Exception as exc:
                    errors.append(exc)

            thread = threading.Thread(target=second)
            thread.start()
            thread.join(timeout=40)
            if thread.is_alive():
                raise proof.ProofError("second backup did not finish while the first snapshot was open")

        result = self.run_backup(database, media, self.root / "first-backup", after_ready=after_ready)
        self.assertTrue(any(isinstance(exc, proof.ProofBusy) for exc in errors), proof.redact(errors))
        self.assertFalse((self.root / "second-backup").exists())
        self.assertIn(result.publication, {"exclusive-rename", "platform-rename-proof-only"})
        backup.verify_backup(result.output)

    def test_completed_and_failed_attempts_release_database_snapshot_lock(self):
        database = self.clone("released")
        media = self.root / "media"
        media.mkdir()
        self.seed_original(database, media, "uploads/one.png", png(6))
        self.run_backup(database, media, self.root / "complete")
        self.assertEqual(self.psql(database, ["-c", "SELECT pg_try_advisory_lock(87203461,102341)"]), "t")
        (media / "uploads" / "one.png").unlink()
        with self.assertRaisesRegex(proof.ProofError, "missing referenced media"):
            self.run_backup(database, media, self.root / "failed")
        self.assertEqual(self.psql(database, ["-c", "SELECT pg_try_advisory_lock(87203461,102341)"]), "t")
        self.assertEqual(self.psql(database, ["-c", (
            "SELECT count(*) FROM pg_stat_activity "
            "WHERE datname = current_database() AND pid <> pg_backend_pid()"
        )]), "0")

    def test_concurrent_uploads_and_derivatives_match_one_snapshot(self):
        if shutil.which("go") is None:
            self.fail("Go is required to exercise the running media worker")
        database = self.clone("live")
        media = self.root / "media"
        media.mkdir()
        binary = self.server_binary()
        proc, base = self.start_server(database, media, binary)
        self.procs.append(proc)
        published = self.upload(base, png(11))
        hidden = self.upload(base, png(12))
        trashed = self.upload(base, png(13))
        ready = self.upload(base, png(14))
        for post in (published, hidden, trashed, ready):
            self.wait_ready(base, post["id"])
        self.request(base, "POST", "/api/posts/moderation", {
            "posts": [{"id": hidden["id"]}], "action": "hide", "reason": "proof",
        })
        self.request(base, "POST", "/api/posts/trash", {"post_ids": [trashed["id"]], "reason": "proof"})
        stop = threading.Event()
        background = []

        def keep_uploading():
            number = 100
            while not stop.is_set():
                number += 1
                try:
                    background.append(self.upload(base, png(number))["id"])
                except (HTTPError, URLError, TimeoutError, OSError):
                    if stop.is_set():
                        return
                stop.wait(0.02)

        worker = threading.Thread(target=keep_uploading)
        worker.start()
        def stop_uploads():
            stop.set()
            worker.join(timeout=15)
        self.addCleanup(stop_uploads)
        deadline = time.monotonic() + 10
        while not background and time.monotonic() < deadline:
            time.sleep(0.05)
        self.assertTrue(background, "uploads were not accepted before the snapshot")
        observed = {}

        def during_snapshot(ready_view):
            (media / "orphan.bin").write_bytes(b"orphan-not-referenced")
            (media / "uploads").mkdir(exist_ok=True)
            (media / "uploads" / ".upload-partial").write_bytes(b"partial-upload")
            pending_key = "uploads/pending-uncommitted.png"
            pending_bytes = png(21)
            proof.link_immutable(media, pending_key, pending_bytes)
            observed["pending_key"] = pending_key
            observed["pending_proc"] = self.open_uncommitted_post(database, pending_key, pending_bytes)
            late_variant = "derivatives/thumb-160/late.jpg"
            late_thumb = b"late-thumb"
            proof.link_immutable(media, late_variant, late_thumb)
            self.psql(database, ["-c", (
                "INSERT INTO asset_variants (post_id, variant, object_key, url, media_type, width, height, file_size, status) "
                f"VALUES ({int(published['id'])}, 'thumb-160', '{late_variant}', '/media/{late_variant}', "
                f"'image/jpeg', 8, 8, {len(late_thumb)}, 'ready')"
            )])
            observed["late_variant"] = late_variant
            self.request(base, "POST", "/api/posts/trash", {"post_ids": [published["id"]], "reason": "during snapshot"})
            late = self.upload(base, png(22))
            observed["late_id"] = late["id"]
            self.wait_ready(base, late["id"])
            observed["late_worker_variants"] = self.json_query(database, (
                "SELECT json_agg(object_key) FROM asset_variants "
                f"WHERE post_id = {int(late['id'])} AND status = 'ready'"
            ))
            self.assertTrue(observed["late_worker_variants"], "real derivative worker did not complete during the snapshot")
            observed["uploads_during_open_snapshot"] = len(background)
            self.assertGreater(int(ready_view.document["posts"][0]["id"]), 0)

        before_background = len(background)
        result = self.run_backup(database, media, self.root / "proof-live", after_ready=during_snapshot)
        self.assertGreaterEqual(len(background), before_background)
        self.assertGreaterEqual(observed["uploads_during_open_snapshot"], 1)
        stop.set()
        worker.join(timeout=15)
        pending_id = observed["pending_proc"]["id"]
        self.stop_procs()
        worker.join(timeout=5)
        live_rows = self.json_query(database, (
            "SELECT COALESCE(json_agg(json_build_object('id', id::text, 'trashed', (deleted_at IS NOT NULL))), '[]'::json) FROM posts"
        ))
        live_by_id = {row["id"]: row for row in live_rows}
        self.assertIn(observed["late_id"], live_by_id)
        self.assertNotIn(pending_id, live_by_id)
        self.assertTrue(live_by_id[published["id"]]["trashed"])
        target = self.restore_dump(result.output / "database.dump")
        restored = self.reference_document(target)
        restored_ids = {str(post["id"]) for post in restored["posts"]}
        self.assertTrue({published["id"], hidden["id"], trashed["id"], ready["id"]} <= restored_ids)
        self.assertNotIn(observed["late_id"], restored_ids)
        self.assertTrue(set(observed["late_worker_variants"]).isdisjoint(result.keys))
        self.assertNotIn(pending_id, restored_ids)
        by_id = {str(post["id"]): post for post in restored["posts"]}
        self.assertFalse(by_id[published["id"]]["trashed"])
        self.assertEqual(by_id[hidden["id"]]["moderation_state"], "hidden")
        self.assertTrue(by_id[trashed["id"]]["trashed"])
        self.assertEqual(set(proof.expectations_from_document(restored)), set(result.keys))
        self.assertNotIn(observed["pending_key"], result.keys)
        self.assertNotIn(observed["late_variant"], result.keys)
        self.assertNotIn("orphan.bin", result.keys)
        self.assertNotIn("uploads/.upload-partial", result.keys)
        for post in restored["posts"]:
            key = backup.referenced_media_key(post["original"])
            blob = (result.output / "media" / Path(key)).read_bytes()
            self.assertEqual(hashlib.sha256(blob).hexdigest(), proof.content_hash(post["hash"]))
            self.assertEqual(len(blob), post["file_size"])
            self.assertEqual(blob, (_media_file(media, key)).read_bytes())
        variants = self.json_query(target, (
            "SELECT COALESCE(json_agg(json_build_object('post_id', post_id::text, 'variant', variant, "
            "'object_key', object_key, 'file_size', file_size, 'status', status)), '[]'::json) FROM asset_variants"
        ))
        ready_variants = [row for row in variants if row["status"] == "ready" and row["variant"] == "thumb-320"]
        self.assertTrue({published["id"], hidden["id"], trashed["id"], ready["id"]} <= {row["post_id"] for row in ready_variants})
        self.assertFalse(any(row["variant"] == "thumb-160" for row in variants))
        for row in ready_variants:
            blob = (result.output / "media" / Path(row["object_key"])).read_bytes()
            self.assertEqual(len(blob), row["file_size"])
            self.assertEqual(blob, _media_file(media, row["object_key"]).read_bytes())
            self.assertTrue(blob.startswith(b"\xff\xd8"))
        job_keys = self.json_query(target, (
            "SELECT COALESCE(json_agg(payload->>'original_key'), '[]'::json) FROM jobs WHERE payload ? 'original_key'"
        ))
        self.assertTrue(set(job_keys) <= set(result.keys))
        self.assertIn(result.publication, {"exclusive-rename", "platform-rename-proof-only"})
        backup.verify_backup(result.output)
        manifest = (result.output / "manifest.json").read_text(encoding="utf-8")
        self.assertNotIn("postgresql://", manifest)
        self.assertNotIn("postgres://", manifest)

    def open_uncommitted_post(self, database, key, data):
        digest = hashlib.sha256(data).hexdigest()
        url = "/media/" + key
        work = self.root / "pending"
        work.mkdir()
        script = work / "pending.sql"
        script.write_text("\n".join([
            "\\pset tuples_only on",
            "\\pset format unaligned",
            "\\pset pager off",
            "BEGIN;",
            f"\\o '{(work / 'id.txt').as_posix()}'",
            "INSERT INTO posts (preview_url, original_url, media_type, width, height, hash, file_size) "
            f"VALUES ('{url}', '{url}', 'image/png', 16, 16, 'sha256:{digest}', {len(data)}) RETURNING id::text;",
            "\\o",
            f"\\o '{(work / 'ready.txt').as_posix()}'",
            "SELECT 'open';",
            "\\o",
            "",
        ]), encoding="utf-8")
        log = (work / "pending.err").open("w", encoding="utf-8")
        self.logs.append(log)
        proc = subprocess.Popen(
            ["psql", "-X", "-v", "ON_ERROR_STOP=1"],
            env=self.proof_env(database), stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=log,
        )
        self.procs.append(proc)
        proc.stdin.write(script.read_bytes())
        proc.stdin.flush()
        deadline = time.monotonic() + 20
        ready = work / "ready.txt"
        while time.monotonic() < deadline:
            if ready.is_file() and ready.stat().st_size > 0:
                return {"id": (work / "id.txt").read_text(encoding="utf-8").strip(), "proc": proc}
            if proc.poll() is not None:
                self.fail("uncommitted post session ended: " + proof.redact((work / "pending.err").read_text(encoding="utf-8", errors="replace")))
            time.sleep(0.05)
        self.fail("uncommitted post session timed out")

    def server_binary(self):
        cached = getattr(self.__class__, "_server_binary", None)
        if cached:
            return cached
        name = "kura-server.exe" if os.name == "nt" else "kura-server"
        binary = Path(self.class_temp.name) / name
        result = subprocess.run(
            ["go", "-C", str(REPO / "server"), "build", "-o", str(binary), "./cmd/kura-server"],
            capture_output=True, text=True,
        )
        if result.returncode != 0:
            self.fail("server build failed: " + proof.redact(result.stderr))
        self.__class__._server_binary = binary
        return binary

    def start_server(self, database, media, binary):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env = self.proof_env(database)
        env["DATABASE_URL"] = self.database_url(database)
        env.pop("KURA_BACKUP_TEST_ADMIN_URL", None)
        env.update(HTTP_ADDR="127.0.0.1:" + str(port), MEDIA_ROOT=str(media), KURA_API_TOKEN=TOKEN)
        log_path = self.root / "server.err"
        log = log_path.open("w", encoding="utf-8")
        self.logs.append(log)
        proc = subprocess.Popen([str(binary)], env=env, stdout=log, stderr=subprocess.STDOUT)
        base = "http://127.0.0.1:" + str(port)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                self.fail("disposable API exited: " + proof.redact(log_path.read_text(encoding="utf-8", errors="replace")))
            try:
                self.request(base, "GET", "/api/posts")
                return proc, base
            except (URLError, HTTPError, TimeoutError):
                time.sleep(0.05)
        self.fail("disposable API did not become ready: " + proof.redact(log_path.read_text(encoding="utf-8", errors="replace")))

    def request(self, base, method, path, body=None):
        data = None
        headers = {"Authorization": "Bearer " + TOKEN}
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        with urlopen(Request(base + path, data=data, headers=headers, method=method), timeout=10) as response:
            return json.loads(response.read().decode())

    def upload(self, base, payload):
        boundary = "kura-online-backup-proof"
        body = (
            "--" + boundary + "\r\n"
            'Content-Disposition: form-data; name="file"; filename="proof.png"\r\n'
            "Content-Type: image/png\r\n\r\n"
        ).encode() + payload + ("\r\n--" + boundary + "--\r\n").encode()
        headers = {
            "Authorization": "Bearer " + TOKEN,
            "Content-Type": "multipart/form-data; boundary=" + boundary,
        }
        with urlopen(Request(base + "/api/uploads", data=body, headers=headers, method="POST"), timeout=20) as response:
            return json.loads(response.read().decode())

    def wait_ready(self, base, post_id):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            detail = self.request(base, "GET", "/api/posts/" + post_id)
            if detail.get("derivative_status") == "succeeded":
                return detail
            time.sleep(0.1)
        self.fail("derivative job did not succeed for a generated upload")


def _media_file(root, key):
    return root.joinpath(*key.split("/"))


if __name__ == "__main__":
    unittest.main()
