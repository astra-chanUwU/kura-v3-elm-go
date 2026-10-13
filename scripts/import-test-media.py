#!/usr/bin/env python3
"""Import supported images from a folder into a running local Kura API."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


MAX_BYTES = 25 * 1024 * 1024
SUPPORTED = {".jpg", ".jpeg", ".png", ".gif"}
VIDEO = {".mp4", ".mov", ".m4v", ".avi", ".mkv", ".webm"}
PACKAGES = {".photoslibrary", ".photolibrary"}
METADATA_NAMES = {".ds_store", "thumbs.db", "desktop.ini", "ehthumbs.db"}
METADATA_SUFFIXES = {".db", ".sqlite", ".sqlite3", ".plist", ".cache"}
PAGE_SIZE = 60
MAX_PAGES = 1000
MAX_JSON_BYTES = 16 * 1024 * 1024
CHUNK_SIZE = 1024 * 1024


class ImportErrorSafe(Exception):
    """Expected fail-closed importer error with a safe user-facing message."""


class SnapshotTooLarge(Exception):
    pass


class SourceChanged(Exception):
    pass


class NoRedirectHandler(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


HTTP = build_opener(NoRedirectHandler())


def validate_api_url(value):
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.query or parsed.fragment or "?" in value or "#" in value
                or any(ord(char) <= 0x20 for char in value)):
            raise ValueError
        _ = parsed.port
    except (ValueError, TypeError):
        raise ImportErrorSafe("API URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
    return value.rstrip("/")


def api_json(url, token=None):
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    try:
        with HTTP.open(Request(url, headers=headers), timeout=30) as response:
            if response.getcode() != 200:
                raise ImportErrorSafe(f"API returned unexpected HTTP status {response.getcode()}")
            raw = response.read(MAX_JSON_BYTES + 1)
    except HTTPError as exc:
        code = exc.code
        exc.close()
        raise ImportErrorSafe(f"API request failed with HTTP {code}")
    if len(raw) > MAX_JSON_BYTES:
        raise ImportErrorSafe("API JSON response exceeds the 16 MiB limit")
    try:
        decoded = json.loads(raw.decode("utf-8"))
    except (UnicodeError, json.JSONDecodeError):
        raise ImportErrorSafe("API returned malformed JSON")
    if not isinstance(decoded, dict):
        raise ImportErrorSafe("API returned a malformed JSON object")
    return decoded


def positive_id(value):
    return isinstance(value, str) and re.fullmatch(r"[1-9][0-9]*", value) is not None


def validate_hash(value):
    return isinstance(value, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", value) is not None


def all_existing_hashes(api_url):
    """Read browse-visible posts only; malformed or unbounded pagination fails closed."""
    hashes = set()
    post_ids = set()
    seen_cursors = set()
    cursor = None
    for page_number in range(1, MAX_PAGES + 1):
        params = {"limit": PAGE_SIZE}
        if cursor:
            params["cursor"] = cursor
        page = api_json(api_url + "/api/posts?" + urlencode(params))
        posts = page.get("posts")
        if not isinstance(posts, list) or len(posts) > PAGE_SIZE:
            raise ImportErrorSafe("Posts API returned a page outside the 0..60 post limit")
        if "next_cursor" not in page:
            raise ImportErrorSafe("Posts API response is missing its pagination cursor field")
        next_cursor = page.get("next_cursor")
        if next_cursor is not None and (not isinstance(next_cursor, str) or not next_cursor.strip()):
            raise ImportErrorSafe("Posts API returned an invalid pagination cursor")
        if not posts and next_cursor is not None:
            raise ImportErrorSafe("Posts API returned an empty page with a continuation cursor")
        for post in posts:
            if not isinstance(post, dict) or not positive_id(post.get("id")):
                raise ImportErrorSafe("Posts API returned a malformed or noncanonical post ID")
            post_id = post["id"]
            if post_id in post_ids:
                raise ImportErrorSafe("Posts API repeated a post ID during pagination")
            post_ids.add(post_id)
            detail = api_json(api_url + "/api/posts/" + post_id)
            if detail.get("id") != post_id:
                raise ImportErrorSafe("Post detail API returned a different or malformed post ID")
            value = detail.get("hash")
            if value == "":
                continue
            if not validate_hash(value):
                raise ImportErrorSafe("Post detail API returned a malformed SHA-256 hash")
            hashes.add(value[7:])
        if next_cursor is None:
            return hashes
        if next_cursor in seen_cursors:
            raise ImportErrorSafe("Posts API repeated a pagination cursor")
        seen_cursors.add(next_cursor)
        if page_number == MAX_PAGES:
            raise ImportErrorSafe(f"Posts API exceeded the {MAX_PAGES}-page safety limit")
        cursor = next_cursor
    raise ImportErrorSafe(f"Posts API exceeded the {MAX_PAGES}-page safety limit")


def scan(root):
    """Yield supported regular files, while summarizing excluded content."""
    counts = {"hidden_files": 0, "hidden_dirs": 0, "package_dirs": 0}
    unsupported = {}
    candidates = []
    def walk_error(error):
        raise error

    for current, dirs, files in os.walk(root, topdown=True, onerror=walk_error, followlinks=False):
        current_path = Path(current)
        allowed_dirs = []
        for name in dirs:
            path = current_path / name
            suffix = path.suffix.lower()
            if name.startswith("."):
                counts["hidden_dirs"] += 1
            elif suffix in PACKAGES or "photos library" in name.lower():
                counts["package_dirs"] += 1
            elif path.is_symlink():
                continue
            else:
                allowed_dirs.append(name)
        dirs[:] = allowed_dirs

        for name in files:
            path = current_path / name
            if name.startswith(".") or name.lower() in METADATA_NAMES or name.startswith("._"):
                counts["hidden_files"] += 1
                continue
            if path.is_symlink() or not path.is_file():
                continue
            suffix = path.suffix.lower()
            if suffix in METADATA_SUFFIXES:
                counts["hidden_files"] += 1
            elif suffix in SUPPORTED:
                candidates.append(path)
            else:
                key = "video" if suffix in VIDEO else "webp" if suffix == ".webp" else (suffix or "[no extension]")
                unsupported[key] = unsupported.get(key, 0) + 1
    candidates.sort(key=lambda p: p.relative_to(root).as_posix().casefold())
    return candidates, counts, unsupported


def multipart_upload(api_url, path, source, token, snapshot_path):
    boundary = "----kura-folder-import-%x" % int(time.time_ns())
    body = bytearray()

    def field(name, value):
        body.extend(("--" + boundary + "\r\n").encode())
        body.extend((f'Content-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n').encode("utf-8"))

    for tag in ("test-data", "folder-test"):
        field("tags", tag)
    field("source", source)
    body.extend(("--" + boundary + "\r\n").encode())
    filename = path.name.replace("\\", "_").replace('"', "_").replace("\r", "_").replace("\n", "_")
    body.extend((f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n').encode("utf-8"))
    body.extend(b"Content-Type: application/octet-stream\r\n\r\n")
    with snapshot_path.open("rb") as snapshot:
        image_bytes = snapshot.read(MAX_BYTES + 1)
    if len(image_bytes) > MAX_BYTES:
        raise ImportErrorSafe("Private upload snapshot exceeds the 25 MiB limit")
    body.extend(image_bytes)
    body.extend(b"\r\n--" + boundary.encode() + b"--\r\n")
    headers = {"Content-Type": "multipart/form-data; boundary=" + boundary, "Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = Request(api_url + "/api/uploads", data=bytes(body), headers=headers, method="POST")
    with HTTP.open(request, timeout=120) as response:
        if response.getcode() != 201:
            raise ImportErrorSafe(f"Upload returned unexpected HTTP status {response.getcode()}")
        raw = response.read(MAX_JSON_BYTES + 1)
    if len(raw) > MAX_JSON_BYTES:
        raise ImportErrorSafe("Upload response exceeds the 16 MiB JSON limit")
    try:
        detail = json.loads(raw.decode("utf-8"))
    except (UnicodeError, json.JSONDecodeError):
        raise ImportErrorSafe("Upload returned malformed JSON")
    if not isinstance(detail, dict):
        raise ImportErrorSafe("Upload returned a malformed JSON object")
    return 201, detail


def validate_write_capability(api_url, token):
    data = api_json(api_url + "/api/capabilities", token=token)
    auth_required = data.get("writes_require_auth")
    can_write = data.get("can_write")
    actor = data.get("actor")
    if type(auth_required) is not bool or type(can_write) is not bool or (actor is not None and not isinstance(actor, str)):
        raise ImportErrorSafe("Capabilities API returned an unknown or malformed write mode")
    if not can_write:
        raise ImportErrorSafe("Kura is locked for writes or the supplied API token is invalid")
    if auth_required and not token:
        raise ImportErrorSafe("Kura requires a valid KURA_API_TOKEN for uploads")
    if can_write and (not isinstance(actor, str) or not actor):
        raise ImportErrorSafe("Capabilities API returned an unknown writable actor")


def file_signature(info):
    return (info.st_mode, info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def snapshot_source(path, snapshot_dir):
    """Copy and hash one stable source into mode-0600 private temporary storage."""
    try:
        path_before = path.lstat()
    except OSError as exc:
        raise SourceChanged("source file could not be inspected") from exc
    if not stat.S_ISREG(path_before.st_mode) or path.is_symlink():
        raise SourceChanged("source is no longer a regular file")
    if path_before.st_size > MAX_BYTES:
        raise SnapshotTooLarge("over 25 MiB")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        source_fd = os.open(path, flags)
    except OSError as exc:
        raise SourceChanged("source could not be opened safely") from exc
    try:
        snapshot_fd, snapshot_name = tempfile.mkstemp(prefix="snapshot-", dir=snapshot_dir)
    except OSError:
        os.close(source_fd)
        raise
    snapshot_path = Path(snapshot_name)
    digest = hashlib.sha256()
    size = 0
    try:
        os.chmod(snapshot_path, 0o600)
        with os.fdopen(source_fd, "rb") as source, os.fdopen(snapshot_fd, "wb") as target:
            before = os.fstat(source.fileno())
            if not stat.S_ISREG(before.st_mode) or file_signature(before) != file_signature(path_before):
                raise SourceChanged("source was replaced before snapshot")
            while True:
                data = source.read(min(CHUNK_SIZE, MAX_BYTES - size + 1))
                if not data:
                    break
                if size + len(data) > MAX_BYTES:
                    raise SnapshotTooLarge("source grew beyond 25 MiB while being read")
                target.write(data)
                digest.update(data)
                size += len(data)
            target.flush()
            os.fsync(target.fileno())
            after = os.fstat(source.fileno())
            try:
                path_after = path.lstat()
            except OSError as exc:
                raise SourceChanged("source disappeared during snapshot") from exc
            if (file_signature(before) != file_signature(after)
                    or file_signature(after) != file_signature(path_after)
                    or path.is_symlink()):
                raise SourceChanged("source changed during snapshot")
        return snapshot_path, size, digest.hexdigest()
    except BaseException:
        try:
            os.close(source_fd)
        except OSError:
            pass
        try:
            os.close(snapshot_fd)
        except OSError:
            pass
        snapshot_path.unlink(missing_ok=True)
        raise


def save_manifest(path, entries, root, api_url):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".folder-import-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump({"version": 1, "source_root": str(root), "api_url": api_url, "files": entries}, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


class ManifestLock:
    """Exclusive cooperative lock; unknown/stale lock files are never removed."""
    def __init__(self, manifest_path):
        self.path = Path(str(manifest_path) + ".lock")
        self.identity = None

    def __enter__(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        try:
            fd = os.open(self.path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError:
            raise ImportErrorSafe("Another import may be using this resume manifest; remove its lock only after confirming no importer is running")
        try:
            with os.fdopen(fd, "w", encoding="ascii") as stream:
                stream.write(str(os.getpid()) + "\n")
            info = self.path.lstat()
            self.identity = (info.st_dev, info.st_ino)
            return self
        except BaseException:
            try:
                os.close(fd)
            except OSError:
                pass
            try:
                self.path.unlink()
            except OSError:
                pass
            raise

    def __exit__(self, *_):
        try:
            info = self.path.lstat()
            if self.identity == (info.st_dev, info.st_ino):
                self.path.unlink()
        except FileNotFoundError:
            pass


MANIFEST_STATUSES = {
    "pending", "uncertain", "imported", "confirmed-after-resume",
    "confirmed-after-uncertain-response", "duplicate", "oversized", "failed",
}
PROTECTED_STATUSES = {
    "pending", "uncertain", "imported", "confirmed-after-resume",
    "confirmed-after-uncertain-response",
}


def load_manifest(path, root, api_url):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return {}
    if not stat.S_ISREG(info.st_mode) or path.is_symlink():
        raise ImportErrorSafe("Resume manifest is not a regular file; refusing to replace it")

    def unique_object(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON key")
            result[key] = value
        return result

    try:
        with path.open("r", encoding="utf-8") as stream:
            manifest = json.load(stream, object_pairs_hook=unique_object)
    except (OSError, UnicodeError, json.JSONDecodeError, ValueError):
        raise ImportErrorSafe("Resume manifest is unreadable or corrupt; refusing to replace it")
    if not isinstance(manifest, dict) or set(manifest) != {"version", "source_root", "api_url", "files"}:
        raise ImportErrorSafe("Resume manifest has an unsupported structure; refusing to replace it")
    entries = manifest.get("files")
    if (manifest.get("version") != 1 or manifest.get("source_root") != str(root)
            or manifest.get("api_url") != api_url or not isinstance(entries, dict)):
        raise ImportErrorSafe("Resume manifest belongs to another source/API or has an unsupported version")
    allowed_fields = {"status", "sha256", "size", "post_id", "reason", "last_seen"}
    for relative, entry in entries.items():
        if (not isinstance(relative, str) or not relative or "\\" in relative
                or Path(relative).is_absolute() or any(part in {"", ".", ".."} for part in relative.split("/"))):
            raise ImportErrorSafe("Resume manifest contains an unsafe relative path")
        if not isinstance(entry, dict) or set(entry) - allowed_fields or entry.get("status") not in MANIFEST_STATUSES:
            raise ImportErrorSafe("Resume manifest contains a malformed file entry")
        if "sha256" in entry and (not isinstance(entry["sha256"], str) or re.fullmatch(r"[0-9a-f]{64}", entry["sha256"]) is None):
            raise ImportErrorSafe("Resume manifest contains a malformed file hash")
        if "size" in entry and (type(entry["size"]) is not int or entry["size"] < 0):
            raise ImportErrorSafe("Resume manifest contains a malformed file size")
        if "post_id" in entry and not positive_id(entry["post_id"]):
            raise ImportErrorSafe("Resume manifest contains a malformed post ID")
        if "reason" in entry and not isinstance(entry["reason"], str):
            raise ImportErrorSafe("Resume manifest contains a malformed failure reason")
        if "last_seen" in entry and not isinstance(entry["last_seen"], str):
            raise ImportErrorSafe("Resume manifest contains malformed duplicate metadata")
        if entry["status"] in PROTECTED_STATUSES and "sha256" not in entry:
            raise ImportErrorSafe("Resume manifest protected entry is missing its hash")
    return entries


def run_import(folder, api_url, manifest_path, token=None):
    root = Path(folder).expanduser().resolve()
    if not root.is_dir():
        raise ImportErrorSafe("Source folder must be an existing directory")
    api_url = validate_api_url(api_url)
    token = token.strip() if token else None
    manifest_path = Path(manifest_path).expanduser()

    health = api_json(api_url + "/health")
    if health.get("status") != "ok":
        raise ImportErrorSafe("API health check did not return status=ok")
    validate_write_capability(api_url, token)
    existing = all_existing_hashes(api_url)
    candidates, excluded, unsupported = scan(root)

    tally = {"imported": 0, "duplicates": 0, "oversized": 0, "failed": 0, "uncertain": 0}
    failures = []
    with ManifestLock(manifest_path):
        entries = load_manifest(manifest_path, root, api_url)
        unresolved = [
            relative for relative, entry in entries.items()
            if entry["status"] in PROTECTED_STATUSES and entry["sha256"] not in existing
        ]
        if unresolved:
            names = ", ".join(unresolved[:5])
            suffix = "" if len(unresolved) <= 5 else f" (and {len(unresolved) - 5} more)"
            raise ImportErrorSafe(
                "Protected resume entries are absent from browse-visible posts; "
                f"refusing uploads: {names}{suffix}"
            )

        reconciled = False
        for entry in entries.values():
            if entry["status"] in {"pending", "uncertain"} and entry["sha256"] in existing:
                entry["status"] = "confirmed-after-resume"
                reconciled = True
        if reconciled:
            save_manifest(manifest_path, entries, root, api_url)

        with tempfile.TemporaryDirectory(prefix="kura-import-snapshots-") as snapshots:
            for path in candidates:
                relative = path.relative_to(root).as_posix()
                previous = entries.get(relative, {})
                protected = previous.get("status") in PROTECTED_STATUSES
                snapshot_path = None
                try:
                    try:
                        snapshot_path, size, sha = snapshot_source(path, snapshots)
                    except SnapshotTooLarge:
                        if protected:
                            tally["uncertain"] += 1
                            failures.append((relative, "prior upload is unresolved and source changed size; refusing to retry"))
                            break
                        tally["oversized"] += 1
                        failures.append((relative, "over 25 MiB"))
                        entries[relative] = {"status": "oversized", "size": path.stat().st_size}
                        save_manifest(manifest_path, entries, root, api_url)
                        continue
                    except (SourceChanged, OSError):
                        if protected:
                            tally["uncertain"] += 1
                            failures.append((relative, "prior upload is unresolved and source is unavailable or changing; refusing to retry"))
                            break
                        tally["failed"] += 1
                        failures.append((relative, "source changed or could not be read safely"))
                        entries[relative] = {"status": "failed", "reason": "source changed or could not be read safely"}
                        save_manifest(manifest_path, entries, root, api_url)
                        continue

                    if protected:
                        if previous.get("sha256") != sha or sha not in existing:
                            tally["uncertain"] += 1
                            failures.append((relative, "prior upload is unresolved or no longer visible; refusing to retry"))
                            break
                        tally["duplicates"] += 1
                        if previous.get("status") in {"pending", "uncertain"}:
                            entries[relative] = {**previous, "status": "confirmed-after-resume"}
                            save_manifest(manifest_path, entries, root, api_url)
                        continue

                    if sha in existing:
                        tally["duplicates"] += 1
                        if previous.get("sha256") == sha and previous.get("post_id"):
                            entries[relative] = {**previous, "last_seen": "duplicate"}
                        else:
                            entries[relative] = {"status": "duplicate", "sha256": sha}
                        save_manifest(manifest_path, entries, root, api_url)
                        continue

                    source = "folder-test/" + relative.replace("\r", " ").replace("\n", " ")
                    if len(source) > 512:
                        tally["failed"] += 1
                        failures.append((relative, "relative source metadata exceeds 512 characters"))
                        entries[relative] = {"status": "failed", "sha256": sha, "reason": "source too long"}
                        save_manifest(manifest_path, entries, root, api_url)
                        continue

                    entries[relative] = {"status": "pending", "sha256": sha, "size": size}
                    save_manifest(manifest_path, entries, root, api_url)
                    ambiguous = None
                    try:
                        status, detail = multipart_upload(api_url, path, source, token, snapshot_path)
                        if (status != 201 or not validate_hash(detail.get("hash"))
                                or detail["hash"] != "sha256:" + sha or not positive_id(detail.get("id"))):
                            raise ImportErrorSafe("Upload response did not confirm the expected hash and post ID")
                        existing.add(sha)
                        tally["imported"] += 1
                        entries[relative] = {"status": "imported", "sha256": sha, "size": size, "post_id": detail["id"]}
                    except HTTPError as exc:
                        code = exc.code
                        exc.close()
                        if code in {401, 403}:
                            tally["failed"] += 1
                            reason = f"HTTP {code}: write authorization failed; stopping later uploads"
                            entries[relative] = {"status": "failed", "sha256": sha, "size": size, "reason": reason}
                            failures.append((relative, reason))
                            save_manifest(manifest_path, entries, root, api_url)
                            break
                        if 400 <= code < 500 and not 300 <= code < 400:
                            tally["failed"] += 1
                            reason = f"HTTP {code}: upload was rejected"
                            entries[relative] = {"status": "failed", "sha256": sha, "size": size, "reason": reason}
                            failures.append((relative, reason))
                            save_manifest(manifest_path, entries, root, api_url)
                            continue
                        ambiguous = f"HTTP {code}: upload result may be ambiguous"
                    except (URLError, TimeoutError, OSError, RuntimeError, ValueError, ImportErrorSafe) as exc:
                        ambiguous = str(exc) or "upload response was ambiguous"

                    if ambiguous is not None:
                        try:
                            existing = all_existing_hashes(api_url)
                        except Exception:
                            tally["uncertain"] += 1
                            reason = "upload result could not be checked against browse-visible posts; refusing to retry"
                            entries[relative] = {"status": "uncertain", "sha256": sha, "size": size, "reason": reason}
                            save_manifest(manifest_path, entries, root, api_url)
                            failures.append((relative, reason))
                            break
                        if sha in existing:
                            tally["duplicates"] += 1
                            entries[relative] = {"status": "confirmed-after-uncertain-response", "sha256": sha, "size": size}
                        else:
                            tally["uncertain"] += 1
                            reason = "hash is absent from browse-visible posts after an ambiguous upload; refusing to retry"
                            failures.append((relative, reason))
                            entries[relative] = {"status": "uncertain", "sha256": sha, "size": size, "reason": reason}
                            save_manifest(manifest_path, entries, root, api_url)
                            break
                    save_manifest(manifest_path, entries, root, api_url)
                finally:
                    if snapshot_path is not None:
                        try:
                            snapshot_path.unlink()
                        except FileNotFoundError:
                            pass

    print(f"Scanned supported images: {len(candidates)}")
    print("Imported: {imported}; duplicates: {duplicates}; oversized: {oversized}; failed: {failed}; uncertain: {uncertain}".format(**tally))
    print("Excluded hidden files: {hidden_files}; hidden directories: {hidden_dirs}; photo-library packages: {package_dirs}".format(**excluded))
    if unsupported:
        print("Unsupported files: " + ", ".join(f"{key}={value}" for key, value in sorted(unsupported.items())))
    else:
        print("Unsupported files: 0")
    if failures:
        print("Per-file failures (relative paths):")
        for relative, reason in failures:
            print(f"  {relative}: {reason}")
    print(f"Resume manifest: {manifest_path}")
    return 2 if tally["uncertain"] else 1 if tally["failed"] or tally["oversized"] else 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("folder", type=Path, help="source folder to scan (read-only)")
    parser.add_argument("--api-url", default="http://127.0.0.1:8080", help="absolute Kura API HTTP(S) base URL; dedupe covers browse-visible posts")
    parser.add_argument("--manifest", type=Path, help="resume manifest (default: ignored .local/test-media-manifest.json)")
    args = parser.parse_args(argv)
    manifest_path = args.manifest or Path(__file__).resolve().parents[1] / ".local/test-media-manifest.json"
    try:
        return run_import(args.folder, args.api_url, manifest_path, os.environ.get("KURA_API_TOKEN"))
    except (ImportErrorSafe, OSError, URLError, TimeoutError) as exc:
        print(f"Cannot safely import folder: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
