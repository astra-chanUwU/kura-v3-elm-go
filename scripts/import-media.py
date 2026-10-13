#!/usr/bin/env python3
"""Import ordinary image folders into a running Kura API."""

import argparse
import importlib.util
import os
from pathlib import Path
import sys
from urllib.error import URLError


ENGINE_PATH = Path(__file__).with_name("import-test-media.py")
SPEC = importlib.util.spec_from_file_location("kura_import_engine", ENGINE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("Could not load the shared import reliability engine")
engine = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(engine)


def build_parser():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("folder", type=Path, help="source folder to scan (read-only)")
    parser.add_argument("--api-url", default="http://127.0.0.1:8080", help="absolute Kura API HTTP(S) base URL")
    parser.add_argument("--manifest", type=Path, help="resume manifest (default: ignored .local/import-media-manifest.json)")
    parser.add_argument("--tag", action="append", default=[], metavar="TAG", help="tag to apply; may be repeated")
    parser.add_argument("--artist", default="", help="artist metadata to apply to each imported image")
    parser.add_argument("--source-prefix", default="folder-import", help="relative source prefix (default: folder-import)")
    parser.add_argument("--collection", metavar="ID", help="attach imported and duplicate posts to this existing collection")
    parser.add_argument("--dry-run", action="store_true", help="preview each file without creating a manifest or making writes")
    parser.add_argument("--duplicate-policy", choices=("skip", "error"), default="skip",
                        help="skip visible duplicate hashes (default) or fail before uploads")
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    manifest = args.manifest or Path(__file__).resolve().parents[1] / ".local/import-media-manifest.json"
    try:
        return engine.run_import_media(
            args.folder,
            args.api_url,
            manifest,
            os.environ.get("KURA_API_TOKEN"),
            tags=args.tag,
            artist=args.artist,
            source_prefix=args.source_prefix,
            collection_id=args.collection,
            duplicate_policy=args.duplicate_policy,
            dry_run=args.dry_run,
        )
    except (engine.ImportErrorSafe, OSError, URLError, TimeoutError) as exc:
        print(f"Cannot safely import folder: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
