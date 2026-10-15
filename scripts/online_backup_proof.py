#!/usr/bin/env python3
"""Proof harness for an online local backup. This is not product behavior.

The candidate reads one PostgreSQL snapshot, dumps that same snapshot, and
copies only the media keys that snapshot references. Publication uses the
existing manifest format and exclusive_rename. Callers on platforms without
that rename may opt in to a proof-only rename after verification; production
must keep refusing those platforms.
"""

from __future__ import annotations

import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tempfile
import time


SCRIPT = Path(__file__).with_name("library-backup.py")
SPEC = importlib.util.spec_from_file_location("library_backup_for_online_proof", SCRIPT)
backup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(backup)

# Stable per library so two API processes cannot publish two backups at once.
# Barrier keys are per attempt and are not this lock.
BACKUP_LOCK_KEYS = (87203461, 102341)
STAGING_PREFIX = ".kura-backup-"
SNAPSHOT_ID = __import__("re").compile(r"^[0-9A-F]+-[0-9A-F]+-[0-9A-F]+$")
REFERENCE_SQL = """
SELECT json_build_object(
    'posts', COALESCE((SELECT json_agg(json_build_object(
        'id', id,
        'original', original_url,
        'preview', preview_url,
        'hash', hash,
        'file_size', file_size,
        'moderation_state', moderation_state,
        'trashed', (deleted_at IS NOT NULL)
    )) FROM posts), '[]'::json),
    'variants', COALESCE((SELECT json_agg(json_build_object(
        'post_id', post_id,
        'object_key', object_key,
        'url', url,
        'file_size', file_size,
        'status', status
    )) FROM asset_variants), '[]'::json),
    'job_keys', COALESCE((SELECT json_agg(payload->>'original_key')
        FROM jobs
        WHERE payload ? 'original_key'
          AND COALESCE(payload->>'original_key', '') <> ''), '[]'::json)
)::text
""".strip()


class ProofError(backup.BackupError):
    pass


class ProofBusy(ProofError):
    pass


class Expectation:
    def __init__(self):
        self.sha256 = None
        self.size = None
        self.roles = set()


def redact(text):
    """Drop lines that can carry a URL or password. Safe to put in test output."""
    if not text:
        return ""
    kept = []
    for line in str(text).splitlines():
        lowered = line.lower()
        if "://" in line or "password" in lowered or "pgpassword" in lowered:
            kept.append("[redacted]")
        else:
            kept.append(line)
    return "\n".join(kept)[:800]


def content_hash(value):
    if not isinstance(value, str) or not value.startswith("sha256:"):
        return None
    digest = value[len("sha256:"):]
    if len(digest) == 64 and all(c in "0123456789abcdef" for c in digest):
        return digest
    return None


def link_immutable(root, key, data):
    """Publish one local object the way LocalStore.Put does: write a temp file,
    hard-link it into place, and never overwrite an existing key."""
    _reject_key(key)
    path = _key_path(root, key)
    if path.exists() or path.is_symlink():
        if path.is_symlink() or not path.is_file():
            raise ProofError("refusing to replace a media key that is not a regular file")
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=".object-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        try:
            os.link(temp_name, path)
        except FileExistsError:
            return False
        return True
    finally:
        try:
            os.remove(temp_name)
        except FileNotFoundError:
            pass


def expectations_from_document(document):
    """Media keys referenced by one snapshot document.

    posts.preview_url is stored as the original. Served thumbnails come from
    asset_variants and are included here. Hidden and trashed posts are included.
    Job payload keys are included. Nothing is inferred from a directory walk.
    """
    if not isinstance(document, dict):
        raise ProofError("snapshot media document is malformed")
    expectations = {}

    def add(reference, role, sha=None, size=None):
        if reference is None:
            return
        key = backup.referenced_media_key(reference)
        item = expectations.setdefault(key, Expectation())
        item.roles.add(role)
        if sha is not None:
            if item.sha256 is not None and item.sha256 != sha:
                raise ProofError("snapshot hashes disagree for one media key")
            item.sha256 = sha
        if size is not None:
            if item.size is not None and item.size != size:
                raise ProofError("snapshot sizes disagree for one media key")
            item.size = size

    posts = document.get("posts")
    variants = document.get("variants")
    job_keys = document.get("job_keys")
    if not isinstance(posts, list) or not isinstance(variants, list) or not isinstance(job_keys, list):
        raise ProofError("snapshot media document is malformed")
    for post in posts:
        original = post.get("original")
        digest = content_hash(post.get("hash"))
        size = post.get("file_size")
        if not isinstance(size, int) or isinstance(size, bool) or size < 0:
            raise ProofError("snapshot post size is malformed")
        add(original, "original", sha=digest, size=size)
        add(post.get("preview"), "preview")
    for variant in variants:
        size = variant.get("file_size")
        if not isinstance(size, int) or isinstance(size, bool) or size < 0:
            raise ProofError("snapshot variant size is malformed")
        add(variant.get("object_key"), "variant", size=size)
        add(variant.get("url"), "variant", size=size)
    for key in job_keys:
        add(key, "job")
    return expectations


def run_online_backup(media_root, output, open_snapshot, *, after_ready=None, before_dump=None,
                      before_copy=None, copy_impl=None, fail_after_copies=None,
                      allow_platform_rename=False):
    """Create one backup from an already-open snapshot provider.

    open_snapshot(protocol_dir) returns an object with document, snapshot_id,
    busy, dump_to(path), and close(). The provider must keep the exported
    snapshot alive until close().
    """
    backup.check_local_configuration()
    media = backup.ensure_real_directory(backup.absolute_path(media_root), "Media root")
    destination = backup.absolute_path(output)
    if destination.exists() or destination.is_symlink():
        raise ProofError("backup destination already exists")
    backup.reject_symlink_ancestors(destination.parent, "Backup destination")
    destination.parent.mkdir(parents=True, exist_ok=True)
    parent = destination.parent.resolve()
    destination = parent / destination.name
    if destination == media or media in destination.parents or destination in media.parents:
        raise ProofError("backup destination cannot overlap the media root")

    hold = None
    protocol = None
    stage = None
    published = False
    try:
        protocol = Path(tempfile.mkdtemp(prefix=".kura-proof-snap-", dir=parent))
        os.chmod(protocol, 0o700)
        hold = open_snapshot(protocol)
        if getattr(hold, "busy", False):
            raise ProofBusy("a library backup is already running")
        if after_ready is not None:
            after_ready(_Ready(hold.document, media, hold.snapshot_id))
        expectations = expectations_from_document(hold.document)
        pinned = _pin_media(media, expectations)
        if before_dump is not None:
            before_dump(hold)
        stage = Path(tempfile.mkdtemp(prefix=STAGING_PREFIX, dir=parent))
        os.chmod(stage, 0o700)
        dump_path = stage / "database.dump"
        hold.dump_to(dump_path)
        if not dump_path.is_file() or dump_path.is_symlink() or dump_path.stat().st_size <= 0:
            raise ProofError("database dump was not a non-empty regular file")
        os.chmod(dump_path, 0o600)
        if before_copy is not None:
            before_copy()
        entries = [_entry(dump_path, "database.dump")]
        copier = copy_impl or backup.copy_and_hash
        copied = 0
        for key in sorted(pinned):
            if fail_after_copies is not None and copied >= fail_after_copies:
                raise ProofError("backup interrupted")
            size, sha, signature = pinned[key]
            source = media / PurePosixPath(key)
            target = stage / "media" / PurePosixPath(key)
            target.parent.mkdir(parents=True, exist_ok=True)
            written, digest = _copy_pinned(copier, source, target, size, sha, signature)
            if written != size or digest != sha:
                raise ProofError("pinned media hash does not match copied bytes")
            entries.append({"path": "media/" + key, "size": written, "sha256": digest})
            copied += 1
        _write_manifest(stage, entries)
        publication = _publish(stage, destination, allow_platform_rename)
        published = True
        return _Result(destination, publication, tuple(sorted(pinned)), hold.snapshot_id)
    finally:
        if hold is not None:
            hold.close()
        if protocol is not None and protocol.exists():
            shutil.rmtree(protocol, ignore_errors=True)
        if not published and stage is not None and stage.exists():
            shutil.rmtree(stage, ignore_errors=True)


def open_postgres_snapshot(env, *, external_lock=False):
    """Return an opener that exports one REPEATABLE READ snapshot.

    The snapshot transaction stays open through pg_dump. A second opener for
    the same database gets the advisory lock's busy result and does not dump.
    external_lock means the caller already holds that lock for this attempt.
    """
    def opener(protocol):
        session = PostgresSnapshot(env, Path(protocol), external_lock)
        try:
            session.open()
        except Exception:
            session.close()
            raise
        return session
    return opener


class PostgresSnapshot:
    def __init__(self, env, work, external_lock=False):
        self.env = env
        self.work = work
        self.external_lock = external_lock
        self.document = None
        self.snapshot_id = None
        self.busy = False
        self._procs = []
        self._logs = []
        self._closed = False

    def open(self):
        # psql waits for more input while the exported snapshot is open. EOF
        # releases the transaction even when the Python parent is terminated.
        self._snapshot = self._spawn("snapshot.sql", _snapshot_sql(self.work, self.external_lock))
        marker = self._read_marker(self.work / "ready.txt", "snapshot", self._snapshot)
        if marker == "busy":
            self.busy = True
            return
        if marker != "ready":
            raise ProofError("snapshot session returned an unexpected status")
        snapshot_id = (self.work / "snapshot.txt").read_text(encoding="utf-8").strip()
        if not SNAPSHOT_ID.match(snapshot_id):
            raise ProofError("database returned an unusable snapshot id")
        try:
            document = json.loads((self.work / "refs.json").read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            raise ProofError("snapshot media document is unreadable")
        self.snapshot_id = snapshot_id
        self.document = document

    def dump_to(self, path):
        if self.busy or not self.snapshot_id:
            raise ProofError("cannot dump without an exported snapshot")
        database = self.env.get("PGDATABASE", "")
        if not database:
            raise ProofError("snapshot dump is missing its database name")
        _run_tool([
            "pg_dump", "--format=custom", "--no-owner", "--no-acl",
            "--snapshot", self.snapshot_id,
            "--file", str(path),
            "--dbname", database,
        ], self.env, "pg_dump")

    def close(self):
        if self._closed:
            return
        self._closed = True
        for proc in self._procs:
            if proc.stdin is not None:
                try:
                    proc.stdin.close()
                except (OSError, BrokenPipeError):
                    pass
            self._wait(proc)
        for handle in self._logs:
            handle.close()

    def _spawn(self, name, script):
        log = (self.work / (name + ".err")).open("w", encoding="utf-8")
        self._logs.append(log)
        proc = subprocess.Popen(
            ["psql", "-X", "-v", "ON_ERROR_STOP=1"],
            env=self.env, stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=log,
        )
        self._procs.append(proc)
        proc.stdin.write(script.encode("utf-8"))
        proc.stdin.flush()
        return proc

    def _read_marker(self, path, label, proc):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if path.is_file() and path.stat().st_size > 0:
                return path.read_text(encoding="utf-8").strip()
            if proc.poll() is not None:
                raise ProofError(f"{label} session ended early: {self._stderr(label)}")
            time.sleep(0.05)
        raise ProofError(f"{label} session timed out: {self._stderr(label)}")

    def _stderr(self, label):
        path = self.work / f"{label}.sql.err"
        try:
            return redact(path.read_text(encoding="utf-8", errors="replace"))
        except OSError:
            return ""

    @staticmethod
    def _stop(proc):
        if proc is not None and proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)

    @staticmethod
    def _wait(proc):
        if proc is None:
            return
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            PostgresSnapshot._stop(proc)


class _Ready:
    def __init__(self, document, media_root, snapshot_id):
        self.document = document
        self.media_root = media_root
        self.snapshot_id = snapshot_id


class _Result:
    def __init__(self, output, publication, keys, snapshot_id):
        self.output = output
        self.publication = publication
        self.keys = keys
        self.snapshot_id = snapshot_id


def _pin_media(media_root, expectations):
    pinned = {}
    for key, item in expectations.items():
        path = _key_path(media_root, key)
        if path.is_symlink() or not path.is_file():
            raise ProofError(f"missing referenced media: {key}")
        signature = backup.stat_signature(path.lstat())
        with _open_media_file(path) as stream:
            before = backup.stat_signature(os.fstat(stream.fileno()))
            digest = hashlib.sha256()
            size = 0
            for chunk in iter(lambda: stream.read(backup.CHUNK), b""):
                size += len(chunk)
                digest.update(chunk)
            if backup.stat_signature(os.fstat(stream.fileno())) != before or before != signature:
                raise ProofError("referenced media changed during backup")
            sha = digest.hexdigest()
        if backup.stat_signature(path.lstat()) != signature:
            raise ProofError("referenced media changed during backup")
        if item.sha256 is not None and sha != item.sha256:
            raise ProofError(f"referenced original hash does not match the snapshot: {key}")
        if item.size is not None and size != item.size:
            raise ProofError(f"referenced media size does not match the snapshot: {key}")
        pinned[key] = (size, sha, signature)
    return pinned


def _copy_pinned(copier, source, target, size, sha, signature):
    """Copy bytes pinned from the snapshot.

    Handle timestamps and path timestamps disagree on Windows, so the product
    copier's path-versus-handle check cannot be the proof's stability test.
    A custom copier is still honored. The default copier keeps the check on one
    file handle and rejects any hash other than the pin.
    """
    if copier is not backup.copy_and_hash:
        return copier(source, target, signature)
    return _copy_handle_stable(source, target, size, sha)


def _open_media_file(source):
    """Open through directory handles so parent links cannot escape the root."""
    source = Path(source)
    backup.reject_symlink_ancestors(source, "Referenced media")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    if os.open not in os.supports_dir_fd or not hasattr(os, "O_DIRECTORY"):
        return os.fdopen(os.open(source, flags), "rb")
    directory = os.open(source.anchor, flags | os.O_DIRECTORY)
    try:
        for part in source.parts[1:-1]:
            child = os.open(part, flags | os.O_DIRECTORY, dir_fd=directory)
            os.close(directory)
            directory = child
        fd = os.open(source.name, flags, dir_fd=directory)
        return os.fdopen(fd, "rb")
    finally:
        os.close(directory)


def _copy_handle_stable(source, target, size, sha):
    digest = hashlib.sha256()
    total = 0
    src = _open_media_file(source)
    try:
        before = os.fstat(src.fileno())
        if not stat.S_ISREG(before.st_mode):
            raise ProofError("referenced media changed during backup")
        with target.open("xb") as dst:
            os.chmod(target, 0o600)
            while True:
                chunk = src.read(backup.CHUNK)
                if not chunk:
                    break
                dst.write(chunk)
                digest.update(chunk)
                total += len(chunk)
            dst.flush()
            os.fsync(dst.fileno())
        after = os.fstat(src.fileno())
        if backup.stat_signature(before) != backup.stat_signature(after):
            raise ProofError("referenced media changed during backup")
    finally:
        src.close()
    if total != size or digest.hexdigest() != sha:
        raise ProofError("referenced media changed during backup")
    return total, digest.hexdigest()


def _entry(path, rel):
    size, sha = backup.digest_file(path)
    return {"path": rel, "size": size, "sha256": sha}


def _write_manifest(stage, entries):
    manifest = {
        "version": backup.VERSION,
        "created_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "files": entries,
    }
    path = stage / "manifest.json"
    with path.open("x", encoding="utf-8", newline="\n") as stream:
        json.dump(manifest, stream, ensure_ascii=False, sort_keys=True, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.chmod(path, 0o600)


def _publish(stage, destination, allow_platform_rename):
    backup.verify_backup(stage)
    try:
        backup.exclusive_rename(stage, destination)
        return "exclusive-rename"
    except FileExistsError:
        raise ProofError("backup destination was created concurrently")
    except backup.BackupError as exc:
        if "atomic no-overwrite" not in str(exc):
            raise
        if not allow_platform_rename:
            raise ProofError("atomic publication is unavailable on this platform")
        os.rename(stage, destination)
        return "platform-rename-proof-only"


def _run_tool(args, env, label):
    try:
        result = subprocess.run(args, env=env, check=False, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30 * 60)
    except subprocess.TimeoutExpired:
        raise ProofError(f"required tool timed out: {label}")
    except FileNotFoundError:
        raise ProofError(f"required tool is missing: {label}")
    except OSError:
        raise ProofError(f"could not run required tool: {label}")
    if result.returncode != 0:
        detail = redact(result.stderr.decode("utf-8", "replace"))
        raise ProofError(f"{label} failed (exit {result.returncode}): {detail}")


def _reject_key(key):
    parts = key.split("/")
    if (not key or "\\" in key or key.startswith("/") or any(part in {"", ".", ".."} for part in parts)):
        raise ProofError("unsafe media key")


def _key_path(root, key):
    _reject_key(key)
    return root.joinpath(*key.split("/"))


def _psql_quote(path):
    return Path(path).as_posix().replace("'", "''")


def _psql_preamble():
    return "\n".join([
        "\\pset tuples_only on",
        "\\pset format unaligned",
        "\\pset pager off",
        "",
    ])


def _snapshot_sql(work, external_lock=False):
    snap = _psql_quote(work / "snapshot.txt")
    refs = _psql_quote(work / "refs.json")
    ready = _psql_quote(work / "ready.txt")
    lock_a, lock_b = BACKUP_LOCK_KEYS
    statements = [
        "SET statement_timeout = '30s';",
        "SET idle_in_transaction_session_timeout = '35min';",
    ]
    if not external_lock:
        statements += [
            f"SELECT CASE WHEN pg_try_advisory_lock({lock_a}, {lock_b}) THEN 1 ELSE 0 END AS got_lock \\gset",
            "\\if :got_lock",
        ]
    statements += [
        "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;",
        f"\\o '{snap}'",
        "SELECT pg_export_snapshot();",
        "\\o",
        f"\\o '{refs}'",
        REFERENCE_SQL + ";",
        "\\o",
        f"\\o '{ready}'",
        "SELECT 'ready';",
        "\\o",
    ]
    if not external_lock:
        statements += [
            "\\else",
            f"\\o '{ready}'",
            "SELECT 'busy';",
            "\\o",
            "\\endif",
        ]
    # No COMMIT here: psql's idle command loop owns the transaction until
    # close() closes stdin. Unlike pg_sleep, it observes parent pipe closure.
    return _psql_preamble() + "\n".join(statements) + "\n"
