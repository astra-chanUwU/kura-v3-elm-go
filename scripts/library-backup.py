#!/usr/bin/env python3
"""Create, verify, and restore a local Kura PostgreSQL/media backup.

All Kura API/worker processes and every other database or media writer must be
stopped for the complete operation. --offline is an operator acknowledgment;
this utility does not acquire a cross-process lock.

Requires Python 3.9+ and compatible psql, pg_dump, and pg_restore tools on PATH.
DATABASE_URL identifies the source database for create and the target database
for restore; it is never written to the backup manifest.
"""

import argparse
import ctypes
import datetime as dt
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import shutil
import stat
import subprocess
import sys
import tempfile
from urllib.parse import parse_qsl, unquote, urlsplit


VERSION = 1
CHUNK = 1024 * 1024
CORE_TABLES = {"posts", "users"}


class BackupError(Exception):
    pass


def pg_environment(database_url):
    """Parse URL into libpq env without ever putting credentials in argv."""
    try:
        parsed = urlsplit(database_url)
        if parsed.scheme not in {"postgres", "postgresql"} or not parsed.path.strip("/"):
            raise ValueError
        database = unquote(parsed.path.lstrip("/"))
        if not database or "/" in database or "\x00" in database:
            raise ValueError
        env = {k: v for k, v in os.environ.items() if not k.startswith("PG") and k != "DATABASE_URL"}
        env["PGDATABASE"] = database
        if parsed.hostname:
            env["PGHOST"] = parsed.hostname
        if parsed.port:
            env["PGPORT"] = str(parsed.port)
        if parsed.username is not None:
            env["PGUSER"] = unquote(parsed.username)
        if parsed.password is not None:
            env["PGPASSWORD"] = unquote(parsed.password)
        query_items = parse_qsl(parsed.query, keep_blank_values=True, strict_parsing=True) if parsed.query else []
        query = dict(query_items)
        if len(query) != len(query_items):
            raise ValueError
        options = {
            "host": "PGHOST", "port": "PGPORT", "user": "PGUSER", "password": "PGPASSWORD",
            "sslmode": "PGSSLMODE", "sslrootcert": "PGSSLROOTCERT", "sslcert": "PGSSLCERT",
            "sslkey": "PGSSLKEY", "sslcrl": "PGSSLCRL", "connect_timeout": "PGCONNECT_TIMEOUT",
            "application_name": "PGAPPNAME", "target_session_attrs": "PGTARGETSESSIONATTRS",
            "service": "PGSERVICE", "servicefile": "PGSERVICEFILE", "options": "PGOPTIONS",
        }
        if set(query) - options.keys():
            raise ValueError
        for key, value in query.items():
            env[options[key]] = value
        return database, env
    except (ValueError, TypeError):
        raise BackupError("DATABASE_URL must be an absolute postgres:// or postgresql:// URL with an explicit database")


def run_pg(args, env, label, *, capture=False):
    try:
        result = subprocess.run(args, env=env, check=False, text=True,
                                stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL)
    except FileNotFoundError:
        raise BackupError(f"Required PostgreSQL tool is missing: {label}")
    except OSError:
        raise BackupError(f"Could not run required PostgreSQL tool: {label}")
    if result.returncode:
        raise BackupError(f"{label} failed (exit {result.returncode}); details were suppressed to protect connection data")
    return result.stdout.strip() if capture else ""


def check_local_configuration():
    remote_settings = ("S3_ENDPOINT", "AWS_S3_ENDPOINT", "S3_BUCKET", "AWS_S3_BUCKET",
                       "S3_URL_PREFIX", "MEDIA_URL_PREFIX", "S3_PUBLIC_URL_PREFIX")
    if any(os.environ.get(k, "").strip() for k in remote_settings):
        raise BackupError("This local-media backup utility does not support configured S3 storage")


def require_offline(enabled):
    if not enabled:
        raise BackupError("Pass --offline only after stopping all Kura API/worker processes and other writers for the full operation")


def absolute_path(value):
    p = Path(value).expanduser()
    if not p.is_absolute():
        raise BackupError("Paths must be absolute")
    return p


def ensure_real_directory(path, label):
    reject_symlink_ancestors(path, label)
    try:
        st = path.lstat()
    except OSError:
        raise BackupError(f"{label} must be an existing real directory")
    if not path.is_dir() or path.is_symlink():
        raise BackupError(f"{label} must be an existing real directory")
    return path.resolve()


def reject_symlink_ancestors(path, label):
    current = Path(path.anchor)
    for part in path.parts[1:]:
        current = current / part
        try:
            if current.is_symlink():
                raise BackupError(f"{label} path cannot pass through a symlink")
        except OSError:
            raise BackupError(f"Could not inspect {label} path")


def walk_regular_tree(root):
    """Return sorted (relative POSIX path, path) files; refuse links/specials."""
    files = []
    inventory = {".": stat_signature(root.lstat())}
    def walk_error(error):
        raise error

    for current, dirs, names in os.walk(root, topdown=True, onerror=walk_error, followlinks=False):
        current_path = Path(current)
        for name in list(dirs):
            p = current_path / name
            stat = p.lstat()
            if not p.is_dir() or p.is_symlink():
                raise BackupError("Media and backup trees may contain only real directories and regular files")
            inventory[p.relative_to(root).as_posix() + "/"] = stat_signature(stat)
        for name in names:
            p = current_path / name
            stat = p.lstat()
            if p.is_symlink() or not p.is_file():
                raise BackupError("Media and backup trees may contain only real directories and regular files")
            rel = p.relative_to(root).as_posix()
            files.append((rel, p, stat_signature(stat)))
            inventory[rel] = stat_signature(stat)
    files.sort(key=lambda item: item[0])
    return files, inventory


def stat_signature(stat):
    return (stat.st_mode, stat.st_dev, stat.st_ino, stat.st_size,
            stat.st_mtime_ns, stat.st_ctime_ns)


def digest_file(path):
    digest = hashlib.sha256()
    size = 0
    fd = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode):
            raise BackupError("Backup and media files must be regular files")
        while True:
            chunk = stream.read(CHUNK)
            if not chunk:
                break
            size += len(chunk)
            digest.update(chunk)
        after = os.fstat(stream.fileno())
        if stat_signature(before) != stat_signature(after):
            raise BackupError("Backup file changed while being verified")
    return size, digest.hexdigest()


def exclusive_rename(source, destination):
    """Atomically rename a directory only if destination does not exist."""
    src_b, dst_b = os.fsencode(source), os.fsencode(destination)
    if sys.platform.startswith("linux"):
        libc = ctypes.CDLL(None, use_errno=True)
        fn = getattr(libc, "renameat2", None)
        if fn:
            rc = fn(-100, ctypes.c_char_p(src_b), -100, ctypes.c_char_p(dst_b), 1)
            if rc == 0:
                return
            error = ctypes.get_errno()
            if error == 17:
                raise FileExistsError(destination)
            raise OSError(error, os.strerror(error), destination)
    elif sys.platform == "darwin":
        libc = ctypes.CDLL("/usr/lib/libSystem.B.dylib", use_errno=True)
        fn = getattr(libc, "renamex_np", None)
        if fn:
            rc = fn(ctypes.c_char_p(src_b), ctypes.c_char_p(dst_b), 4)  # RENAME_EXCL
            if rc == 0:
                return
            error = ctypes.get_errno()
            if error == 17:
                raise FileExistsError(destination)
            raise OSError(error, os.strerror(error), destination)
    raise BackupError("This platform lacks an atomic no-overwrite directory rename; refusing publication")


def check_core_database(env):
    sql = "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('posts','users') AND table_type='BASE TABLE';"
    count = run_pg(["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env, "psql source check", capture=True)
    if count != str(len(CORE_TABLES)):
        raise BackupError("Source database is missing Kura core tables (posts and users)")


def source_references(env):
    has_variants = run_pg(["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c",
                           "SELECT CASE WHEN to_regclass('public.asset_variants') IS NULL THEN '0' ELSE '1' END;"],
                          env, "psql source check", capture=True) == "1"
    has_jobs = run_pg(["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c",
                       "SELECT CASE WHEN to_regclass('public.jobs') IS NULL THEN '0' ELSE '1' END;"],
                      env, "psql source check", capture=True) == "1"
    union = "SELECT original_url AS ref FROM posts UNION ALL SELECT preview_url FROM posts"
    if has_variants:
        union += " UNION ALL SELECT object_key FROM asset_variants UNION ALL SELECT url FROM asset_variants"
    if has_jobs:
        union += " UNION ALL SELECT payload ->> 'original_key' FROM jobs WHERE payload ? 'original_key'"
    sql = f"SELECT COALESCE(json_agg(ref)::text, '[]') FROM ({union}) AS refs;"
    output = run_pg(["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env, "psql source check", capture=True)
    try:
        refs = json.loads(output)
    except json.JSONDecodeError:
        raise BackupError("Could not read local media references from source database")
    if not isinstance(refs, list) or any(not isinstance(ref, str) for ref in refs):
        raise BackupError("Source database returned malformed local media references")
    return refs


def referenced_media_key(reference):
    if reference.startswith("/media/"):
        key = unquote(reference[len("/media/"):])
    elif reference.startswith(("http://", "https://")):
        parsed = urlsplit(reference)
        if parsed.netloc or parsed.query or parsed.fragment or not parsed.path.startswith("/media/"):
            raise BackupError("Source database references remote media; this backup supports local /media/ objects only")
        key = unquote(parsed.path[len("/media/"):])
    else:
        key = reference
    if (not key or "\\" in key or "\x00" in key or key.startswith("/")
            or posixpath.normpath(key) != key or key in {".", ".."} or key.startswith("../")):
        raise BackupError("Source database contains an unsafe local media reference")
    return key


def check_referenced_media(references, inventory):
    available = {path for path in inventory if path != "." and not path.endswith("/")}
    for reference in references:
        if referenced_media_key(reference) not in available:
            raise BackupError("A media object referenced by the source database is missing from the local media root")


def load_verified_backup(root):
    reject_symlink_ancestors(Path(root), "Backup input")
    root = ensure_real_directory(root, "Backup input")
    actual_files, _ = walk_regular_tree(root)
    actual = {rel for rel, _, _ in actual_files}
    if "manifest.json" not in actual or "database.dump" not in actual:
        raise BackupError("Backup is missing manifest.json or database.dump")
    def unique_object(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate manifest key")
            result[key] = value
        return result

    try:
        manifest = json.loads((root / "manifest.json").read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    except (OSError, UnicodeError, json.JSONDecodeError):
        raise BackupError("Backup manifest is unreadable")
    except ValueError:
        raise BackupError("Backup manifest contains duplicate keys")
    if not isinstance(manifest, dict) or set(manifest) != {"version", "created_at", "files"} or manifest.get("version") != VERSION:
        raise BackupError("Unsupported or malformed backup manifest")
    if not isinstance(manifest["created_at"], str):
        raise BackupError("Backup manifest timestamp is malformed")
    try:
        dt.datetime.fromisoformat(manifest["created_at"])
    except ValueError:
        raise BackupError("Backup manifest timestamp is malformed")
    entries = manifest.get("files")
    if not isinstance(entries, list) or not entries:
        raise BackupError("Backup manifest file list is malformed")
    declared = set()
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != {"path", "size", "sha256"}:
            raise BackupError("Backup manifest contains a malformed file entry")
        rel = entry["path"]
        if not isinstance(rel, str) or not rel or "\\" in rel:
            raise BackupError("Backup manifest contains an unsafe path")
        pure = PurePosixPath(rel)
        if pure.is_absolute() or any(part in {"", ".", ".."} for part in rel.split("/")) or rel != pure.as_posix():
            raise BackupError("Backup manifest contains an unsafe path")
        if rel in declared or (rel != "database.dump" and not rel.startswith("media/")):
            raise BackupError("Backup manifest contains duplicate or unexpected paths")
        if not isinstance(entry["size"], int) or isinstance(entry["size"], bool) or entry["size"] < 0:
            raise BackupError("Backup manifest contains an invalid file size")
        if not isinstance(entry["sha256"], str) or len(entry["sha256"]) != 64 or any(c not in "0123456789abcdef" for c in entry["sha256"]):
            raise BackupError("Backup manifest contains an invalid SHA-256")
        declared.add(rel)
    expected = declared | {"manifest.json"}
    if actual != expected:
        raise BackupError("Backup contains missing or extra files")
    for entry in entries:
        size, sha = digest_file(root / entry["path"])
        if size != entry["size"] or sha != entry["sha256"]:
            raise BackupError(f"Backup integrity check failed for {entry['path']}")
    return root, manifest


def verify_backup(root):
    return load_verified_backup(root)[0]


def create_backup(args):
    require_offline(args.offline)
    check_local_configuration()
    database, env = pg_environment(os.environ.get("DATABASE_URL", ""))
    media_root = ensure_real_directory(absolute_path(args.media_root), "Media root")
    output = absolute_path(args.output)
    if output.exists() or output.is_symlink():
        raise BackupError("Backup destination already exists")
    reject_symlink_ancestors(output.parent, "Backup destination")
    output.parent.mkdir(parents=True, exist_ok=True)
    parent = output.parent.resolve()
    output = parent / output.name
    if output == media_root or media_root in output.parents:
        raise BackupError("Backup destination cannot be inside the media root")
    check_core_database(env)
    try:
        files, initial_inventory = walk_regular_tree(media_root)
        check_referenced_media(source_references(env), initial_inventory)
        stage = Path(tempfile.mkdtemp(prefix=".kura-backup-", dir=parent))
        os.chmod(stage, 0o700)
    except OSError:
        raise BackupError("Could not create private backup staging directory")
    try:
        dump_path = stage / "database.dump"
        run_pg(["pg_dump", "--format=custom", "--no-owner", "--no-acl", "--file", str(dump_path), "--dbname", database], env, "pg_dump")
        if not dump_path.is_file() or dump_path.is_symlink():
            raise BackupError("pg_dump did not produce a regular database dump")
        os.chmod(dump_path, 0o600)
        entries = []
        size, sha = digest_file(dump_path)
        entries.append({"path": "database.dump", "size": size, "sha256": sha})
        for rel, source, signature in files:
            target = stage / "media" / PurePosixPath(rel)
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            target.parent.chmod(0o700)
            size, sha = copy_and_hash(source, target, signature)
            entries.append({"path": "media/" + rel, "size": size, "sha256": sha})
        _, final_inventory = walk_regular_tree(media_root)
        if final_inventory != initial_inventory:
            raise BackupError("Media tree changed during backup")
        manifest = {"version": VERSION, "created_at": dt.datetime.now(dt.timezone.utc).isoformat(), "files": entries}
        mpath = stage / "manifest.json"
        with mpath.open("x", encoding="utf-8") as stream:
            json.dump(manifest, stream, ensure_ascii=False, sort_keys=True, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(mpath, 0o600)
        # Stage tree must be verified before publication.
        verify_backup(stage)
        exclusive_rename(stage, output)
        print(f"Backup created at {output}")
    except FileExistsError:
        raise BackupError("Backup destination was created concurrently; existing data was left untouched")
    except BackupError:
        raise
    except Exception:
        raise BackupError("Backup failed; private staging data was cleaned up")
    finally:
        if stage.exists():
            shutil.rmtree(stage, ignore_errors=True)


def copy_and_hash(source, target, expected_signature=None):
    digest = hashlib.sha256()
    size = 0
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    fd = os.open(source, flags)
    with os.fdopen(fd, "rb") as src, target.open("xb") as dst:
        os.chmod(target, 0o600)
        before = os.fstat(src.fileno())
        if not stat.S_ISREG(before.st_mode):
            raise BackupError("Media source changed during backup")
        if expected_signature is not None and stat_signature(before) != expected_signature:
            raise BackupError("Media source changed during backup")
        while True:
            data = src.read(CHUNK)
            if not data:
                break
            dst.write(data)
            digest.update(data)
            size += len(data)
        dst.flush()
        os.fsync(dst.fileno())
        after = os.fstat(src.fileno())
        try:
            path_after = source.lstat()
        except OSError:
            raise BackupError("Media source changed during backup")
        if (stat_signature(before) != stat_signature(after)
                or stat_signature(after) != stat_signature(path_after)):
            raise BackupError("Media source changed during backup")
    return size, digest.hexdigest()


def assert_target_empty(env):
    sql = "SELECT (SELECT count(*) FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema','pg_toast','public') AND nspname !~ '^pg_(temp|toast_temp)_[0-9]+$') || '|' || (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public') || '|' || (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public') || '|' || ((SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_operator o JOIN pg_namespace n ON n.oid=o.oprnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_conversion c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_ts_parser p JOIN pg_namespace n ON n.oid=p.prsnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_ts_template t JOIN pg_namespace n ON n.oid=t.tmplnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_ts_dict d JOIN pg_namespace n ON n.oid=d.dictnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_ts_config c JOIN pg_namespace n ON n.oid=c.cfgnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_event_trigger) + (SELECT count(*) FROM pg_largeobject_metadata) + (SELECT count(*) FROM pg_language WHERE lanname NOT IN ('internal','c','sql','plpgsql')) + (SELECT count(*) FROM pg_default_acl) + (SELECT count(*) FROM pg_publication) + (SELECT count(*) FROM pg_subscription)) || '|' || (SELECT count(*) FROM pg_extension WHERE extname <> 'plpgsql');"
    value = run_pg(["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env, "psql target check", capture=True)
    if value != "0|0|0|0|0":
        raise BackupError("Restore target database must have no user schemas, relations, functions, types, or non-built-in extensions")


def restore_backup(args):
    require_offline(args.offline)
    check_local_configuration()
    backup, manifest = load_verified_backup(absolute_path(args.input))
    reject_symlink_ancestors(absolute_path(args.input), "Backup input")
    database, env = pg_environment(os.environ.get("DATABASE_URL", ""))
    media_target = absolute_path(args.media_root)
    if media_target.exists() or media_target.is_symlink():
        raise BackupError("Restore media destination must not already exist")
    reject_symlink_ancestors(media_target.parent, "Restore media destination")
    media_target.parent.mkdir(parents=True, exist_ok=True)
    media_target = media_target.parent.resolve() / media_target.name
    if backup == media_target or backup in media_target.parents:
        raise BackupError("Restore media destination cannot be inside the backup directory")
    stage = Path(tempfile.mkdtemp(prefix=".kura-restore-", dir=media_target.parent))
    os.chmod(stage, 0o700)
    staged_media = stage / "media"
    staged_media.mkdir(mode=0o700)
    staged_dump = stage / "database.dump"
    db_committed = False
    restore_started = False
    try:
        entries = {entry["path"]: entry for entry in manifest["files"]}
        dump_entry = entries["database.dump"]
        dump_size, dump_sha = copy_and_hash(backup / "database.dump", staged_dump)
        if dump_size != dump_entry["size"] or dump_sha != dump_entry["sha256"]:
            raise BackupError("Restore staging integrity check failed for database.dump")
        for entry in manifest["files"]:
            if not entry["path"].startswith("media/"):
                continue
            rel = entry["path"][len("media/"):]
            target = staged_media / PurePosixPath(rel)
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            size, sha = copy_and_hash(backup / entry["path"], target)
            if size != entry["size"] or sha != entry["sha256"]:
                raise BackupError(f"Restore staging integrity check failed for {entry['path']}")
        run_pg(["pg_restore", "--list", str(staged_dump)], env, "pg_restore archive check")
        target_env = dict(env)
        target_env["PGDATABASE"] = database
        # Check emptiness after staging and immediately before restore writes.
        assert_target_empty(target_env)
        restore_started = True
        run_pg(["pg_restore", "--single-transaction", "--exit-on-error", "--no-owner", "--no-acl", "--dbname", database, str(staged_dump)], target_env, "pg_restore")
        db_committed = True
        try:
            exclusive_rename(staged_media, media_target)
        except Exception:
            raise BackupError(f"Database restore committed, but media publication failed. Recover media from retained staging directory: {staged_media}")
        shutil.rmtree(stage, ignore_errors=True)
        print(f"Database and media restored; media is at {media_target}")
    except KeyboardInterrupt:
        if restore_started or db_committed:
            raise BackupError(f"Restore interrupted; database outcome may be unknown. Staged media is retained at {staged_media}")
        raise
    except BackupError as exc:
        if restore_started and not db_committed:
            raise BackupError(f"Database restore outcome could not be confirmed; staged media is retained at {staged_media}. {exc}")
        raise
    except FileExistsError:
        if db_committed:
            raise BackupError(f"Database restore committed, but media destination was created concurrently. Media is retained at {staged_media}")
        raise BackupError("Restore destination was created concurrently; database was not restored")
    except Exception:
        if db_committed:
            raise BackupError(f"Database restore committed; media is retained at {staged_media}")
        raise BackupError("Restore failed before database commit; target database transaction was rolled back")
    finally:
        if stage.exists() and not restore_started:
            shutil.rmtree(stage, ignore_errors=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    create = sub.add_parser("create", help="create a private local-media and PostgreSQL backup")
    create.add_argument("--output", required=True, help="new absolute backup directory")
    create.add_argument("--media-root", required=True, help="existing absolute local media directory")
    create.add_argument("--offline", action="store_true", help="acknowledge all Kura API/worker and other writers are stopped for the full operation; does not enforce a lock")
    verify = sub.add_parser("verify", help="verify manifest structure and every backup file hash")
    verify.add_argument("--input", required=True, help="absolute backup directory")
    restore = sub.add_parser("restore", help="restore into an empty PostgreSQL database and new media directory")
    restore.add_argument("--input", required=True, help="absolute verified backup directory")
    restore.add_argument("--media-root", required=True, help="new absolute media directory")
    restore.add_argument("--offline", action="store_true", help="acknowledge all Kura API/worker and other writers are stopped for the full operation; does not enforce a lock")
    args = parser.parse_args(argv)
    try:
        if args.command == "create":
            create_backup(args)
        elif args.command == "verify":
            verify_backup(absolute_path(args.input))
            print("Backup integrity verified. The manifest detects accidental changes but does not authenticate the backup.")
        else:
            restore_backup(args)
        return 0
    except BackupError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
