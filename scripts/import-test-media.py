#!/usr/bin/env python3
"""Import supported images from a folder into a running local Kura API."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


MAX_BYTES = 25 * 1024 * 1024
SUPPORTED = {".jpg", ".jpeg", ".png", ".gif"}
VIDEO = {".mp4", ".mov", ".m4v", ".avi", ".mkv", ".webm"}
PACKAGES = {".photoslibrary", ".photolibrary"}
METADATA_NAMES = {".ds_store", "thumbs.db", "desktop.ini", "ehthumbs.db"}
METADATA_SUFFIXES = {".db", ".sqlite", ".sqlite3", ".plist", ".cache"}
PAGE_SIZE = 60


def api_json(url):
    headers = {"Accept": "application/json"}
    with urlopen(Request(url, headers=headers), timeout=30) as response:
        return json.load(response)


def all_existing_hashes(api_url):
    """Read visible post summaries and details so dedupe uses stored hashes."""
    hashes = set()
    cursor = None
    while True:
        params = {"limit": PAGE_SIZE}
        if cursor:
            params["cursor"] = cursor
        page = api_json(api_url + "/api/posts?" + urlencode(params))
        for post in page.get("posts", []):
            detail = api_json(api_url + "/api/posts/" + str(post["id"]))
            value = detail.get("hash", "")
            if value.startswith("sha256:"):
                hashes.add(value[7:].lower())
        cursor = page.get("next_cursor")
        if not cursor:
            return hashes


def scan(root):
    """Yield supported regular files, while summarizing excluded content."""
    counts = {"hidden_files": 0, "hidden_dirs": 0, "package_dirs": 0}
    unsupported = {}
    candidates = []
    for current, dirs, files in os.walk(root, topdown=True, followlinks=False):
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


def multipart_upload(api_url, path, source, token):
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
    body.extend(path.read_bytes())
    body.extend(b"\r\n--" + boundary.encode() + b"--\r\n")
    headers = {"Content-Type": "multipart/form-data; boundary=" + boundary, "Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = Request(api_url + "/api/uploads", data=bytes(body), headers=headers, method="POST")
    with urlopen(request, timeout=120) as response:
        return response.status, json.load(response)


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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("folder", type=Path, help="source folder to scan (read-only)")
    parser.add_argument("--api-url", default="http://127.0.0.1:8080", help="Kura API base URL")
    parser.add_argument("--manifest", type=Path, help="resume manifest (default: ignored .local/test-media-manifest.json)")
    args = parser.parse_args()
    root = args.folder.expanduser().resolve()
    if not root.is_dir():
        parser.error("folder must be an existing directory")
    api_url = args.api_url.rstrip("/")
    token = os.environ.get("KURA_API_TOKEN")
    manifest_path = args.manifest or Path(__file__).resolve().parents[1] / ".local/test-media-manifest.json"
    try:
        health = api_json(api_url + "/health")
        if health.get("status") != "ok":
            raise RuntimeError("API health check did not return status=ok")
        existing = all_existing_hashes(api_url)
    except Exception as exc:
        print(f"Cannot read Kura API or existing post hashes: {exc}", file=sys.stderr)
        return 2

    candidates, excluded, unsupported = scan(root)
    try:
        old_manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        if not isinstance(old_manifest, dict):
            raise ValueError("manifest root must be an object")
        entries = old_manifest.get("files")
        if old_manifest.get("version") != 1 or not isinstance(entries, dict):
            raise ValueError("unsupported or malformed manifest structure")
        if old_manifest.get("source_root") != str(root) or old_manifest.get("api_url") != api_url:
            raise ValueError("manifest belongs to a different source folder or API URL")
        if any(not isinstance(entry, dict) for entry in entries.values()):
            raise ValueError("manifest contains a malformed file entry")
    except FileNotFoundError:
        entries = {}
    except (OSError, ValueError):
        print("Resume manifest is unreadable; refusing to overwrite it.", file=sys.stderr)
        return 2

    tally = {"imported": 0, "duplicates": 0, "oversized": 0, "failed": 0, "uncertain": 0}
    failures = []
    for path in candidates:
        relative = path.relative_to(root).as_posix()
        uncertain_error = None
        try:
            size = path.stat().st_size
            if size > MAX_BYTES:
                tally["oversized"] += 1
                entries[relative] = {"status": "oversized", "size": size}
                save_manifest(manifest_path, entries, root, api_url)
                failures.append((relative, "over 25 MiB"))
                continue
            digest = hashlib.sha256()
            with path.open("rb") as source_file:
                for chunk in iter(lambda: source_file.read(1024 * 1024), b""):
                    digest.update(chunk)
            sha = digest.hexdigest()
        except OSError as exc:
            tally["failed"] += 1
            failures.append((relative, str(exc)))
            entries[relative] = {"status": "failed", "reason": str(exc)}
            save_manifest(manifest_path, entries, root, api_url)
            continue

        previous = entries.get(relative, {})
        protected = {"pending", "uncertain", "imported", "confirmed-after-resume", "confirmed-after-uncertain-response"}
        if previous.get("status") in protected:
            if previous.get("sha256") != sha or sha not in existing:
                tally["uncertain"] += 1
                failures.append((relative, "prior upload is unresolved or no longer visible; refusing to retry"))
                save_manifest(manifest_path, entries, root, api_url)
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
        try:
            status, detail = multipart_upload(api_url, path, source, token)
            returned_hash = detail.get("hash", "")
            if status != 201 or returned_hash != "sha256:" + sha:
                raise RuntimeError("upload response did not confirm the expected SHA-256")
            existing.add(sha)
            tally["imported"] += 1
            entries[relative] = {"status": "imported", "sha256": sha, "post_id": detail.get("id")}
        except HTTPError as exc:
            if exc.code < 500:
                tally["failed"] += 1
                reason = f"HTTP {exc.code}: {exc.reason}"
                failures.append((relative, reason))
                entries[relative] = {"status": "failed", "sha256": sha, "reason": reason}
                save_manifest(manifest_path, entries, root, api_url)
                continue
            uncertain_error = exc
        except (URLError, TimeoutError, OSError, RuntimeError, ValueError) as exc:
            uncertain_error = exc

        if uncertain_error is not None:
            # A lost or ambiguous response may follow a committed upload. Refresh
            # hashes before classifying it; stop if verification is unavailable.
            try:
                existing = all_existing_hashes(api_url)
            except Exception as verify_error:
                tally["uncertain"] += 1
                reason = f"upload result could not be verified: {verify_error}"
                entries[relative] = {"status": "uncertain", "sha256": sha, "reason": reason}
                save_manifest(manifest_path, entries, root, api_url)
                failures.append((relative, reason))
                break
            if sha in existing:
                tally["duplicates"] += 1
                entries[relative] = {"status": "confirmed-after-uncertain-response", "sha256": sha}
            else:
                tally["uncertain"] += 1
                reason = "hash is absent from browse-visible posts after an ambiguous upload; refusing to retry"
                failures.append((relative, reason))
                entries[relative] = {"status": "uncertain", "sha256": sha, "reason": reason}
                save_manifest(manifest_path, entries, root, api_url)
                break
        save_manifest(manifest_path, entries, root, api_url)

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


if __name__ == "__main__":
    raise SystemExit(main())
