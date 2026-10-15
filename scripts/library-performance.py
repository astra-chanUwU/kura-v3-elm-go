#!/usr/bin/env python3
"""Measure representative Kura library queries on a disposable local database."""

import argparse
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import time
from urllib.parse import parse_qs, unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "db" / "migrations"
DATABASE_PREFIX = "kura_perf_"
ALLOWED_ROWS = (10_000, 100_000)


def local_admin_environment(value):
    """Convert a local PostgreSQL URL to libpq environment values, never argv."""
    try:
        parsed = urlsplit(value)
        if parsed.scheme not in ("postgres", "postgresql"):
            raise ValueError
        if not parsed.hostname or not parsed.username or not parsed.path.strip("/"):
            raise ValueError
        if parsed.hostname.lower() != "localhost" and parsed.hostname != "127.0.0.1" and parsed.hostname != "::1":
            raise ValueError
        if parsed.query and set(parse_qs(parsed.query)) - {"sslmode"}:
            raise ValueError
        if parsed.fragment:
            raise ValueError
        port = parsed.port or 5432
        if not (1 <= port <= 65535):
            raise ValueError
        database = unquote(parsed.path.strip("/"))
        if not re.fullmatch(r"[A-Za-z0-9_]+", database):
            raise ValueError
        query = parse_qs(parsed.query)
        sslmode = query.get("sslmode", ["disable"])[0]
        if sslmode not in {"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}:
            raise ValueError
        return {
            "PGHOST": parsed.hostname,
            "PGPORT": str(port),
            "PGUSER": unquote(parsed.username),
            "PGPASSWORD": unquote(parsed.password or ""),
            "PGSSLMODE": sslmode,
            "PGAPPNAME": "kura-library-performance",
        }, database
    except (TypeError, ValueError):
        raise ValueError(
            "KURA_PERF_ADMIN_DATABASE_URL must be a local postgresql URL with a database name"
        ) from None


def generated_database_name(token=None):
    name = DATABASE_PREFIX + (token or secrets.token_hex(12))
    if not re.fullmatch(r"kura_perf_[a-f0-9]{24}", name):
        raise ValueError("invalid generated database name")
    return name


def query_cases(rows):
    deep_cursor = max(1, rows // 10)
    deep_position = rows * 9 // 10
    return [
        (
            "search_rare_term",
            """SELECT id FROM posts
               WHERE deleted_at IS NULL AND moderation_state = 'published'
                 AND search_document @@ websearch_to_tsquery('simple', 'rareanchor')
               ORDER BY id DESC LIMIT 61""",
        ),
        (
            "deep_keyset_page",
            f"""SELECT id FROM posts
                WHERE deleted_at IS NULL AND moderation_state = 'published'
                  AND id < {deep_cursor}
                ORDER BY id DESC LIMIT 61""",
        ),
        (
            "collection_deep_keyset_page",
            f"""SELECT p.id
                FROM collection_posts cp
                JOIN posts p ON p.id = cp.post_id
                  AND p.deleted_at IS NULL AND p.moderation_state = 'published'
                WHERE cp.collection_id = 1
                  AND (cp.position > {deep_position}
                       OR (cp.position = {deep_position} AND cp.post_id > {deep_position + 1}))
                ORDER BY cp.position, cp.post_id LIMIT 61""",
        ),
        (
            "thumbnail_overlay_for_page",
            """SELECT p.id, COALESCE(v.url, p.preview_url)
                FROM (
                    SELECT id, preview_url FROM posts
                    WHERE deleted_at IS NULL AND moderation_state = 'published'
                    ORDER BY id DESC LIMIT 60
                ) p
                LEFT JOIN asset_variants v
                  ON v.post_id = p.id AND v.variant = 'thumb-320' AND v.status = 'ready'
                ORDER BY p.id DESC""",
        ),
    ]


def run_psql(psql, database, environment, sql=None, file=None, step="database operation", timeout=180):
    command = [psql, "-X", "-q", "-v", "ON_ERROR_STOP=1", "--dbname", database]
    if file is not None:
        command.extend(["--file", str(file)])
    else:
        command.extend(["-A", "-t"])
    try:
        result = subprocess.run(
            command,
            input=sql,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=environment,
            timeout=timeout,
            check=False,
        )
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"{step} exceeded {timeout} seconds") from None
    if result.returncode != 0:
        raise RuntimeError(f"{step} failed (psql exit {result.returncode}; connection details suppressed)")
    return result.stdout.strip()


def seed_sql(rows):
    return f"""\
INSERT INTO posts (
    preview_url, original_url, media_type, width, height, search_text,
    source, artist, hash, file_size, tags
)
SELECT
    '/synthetic/media/' || g || '.jpg',
    '/synthetic/media/' || g || '.jpg',
    'image/jpeg',
    1280 + (g % 5) * 120,
    720 + (g % 7) * 90,
    CASE WHEN g % 997 = 0 THEN 'rareanchor synthetic subject' ELSE 'synthetic subject' END
        || ' batchtoken' || (g % 1000),
    'performance-harness/' || (g % 100),
    'synthetic',
    'sha256:' || lpad(to_hex(g), 64, '0'),
    1024 + (g % 500000),
    ARRAY['synthetic', 'batch_' || (g % 100)::text]
FROM generate_series(1, {rows}) AS generated(g);

WITH created AS (
    INSERT INTO collections (name) VALUES ('synthetic performance corpus') RETURNING id
), members AS (
    SELECT id, row_number() OVER (ORDER BY id) - 1 AS position FROM posts
)
INSERT INTO collection_posts (collection_id, post_id, position)
SELECT created.id, members.id, members.position
FROM created CROSS JOIN members;

INSERT INTO asset_variants (
    post_id, variant, object_key, url, media_type, width, height, file_size, status
)
SELECT id, 'thumb-320', 'synthetic/derivatives/' || id || '.jpg',
       '/synthetic/derivatives/' || id || '.jpg', 'image/jpeg',
       320, 180, 512, 'ready'
FROM posts;

ANALYZE posts;
ANALYZE collection_posts;
ANALYZE asset_variants;
"""


def explain_case(psql, database, environment, label, sql):
    started = time.perf_counter()
    raw = run_psql(
        psql,
        database,
        environment,
        sql="EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + sql + ";",
        step=f"measure {label}",
        timeout=180,
    )
    wall_ms = (time.perf_counter() - started) * 1000
    report = json.loads(raw)
    root_plan = report[0]["Plan"]
    actual_rows = int(root_plan.get("Actual Rows", 0) * root_plan.get("Actual Loops", 1))
    planning_ms = float(report[0].get("Planning Time", 0))
    execution_ms = float(report[0].get("Execution Time", 0))
    print(
        f"{label}: rows={actual_rows} planning_ms={planning_ms:.3f} "
        f"execution_ms={execution_ms:.3f} client_wall_ms={wall_ms:.3f}"
    )


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true", help="create, measure, and drop a disposable local database")
    parser.add_argument("--rows", type=int, choices=ALLOWED_ROWS, default=10_000, help="synthetic corpus size (default: 10000)")
    args = parser.parse_args(argv)
    if not args.run:
        parser.error("--run is required; this tool creates and drops a disposable database")
    if not os.environ.get("KURA_PERF_ADMIN_DATABASE_URL"):
        parser.error("set KURA_PERF_ADMIN_DATABASE_URL to an explicit local PostgreSQL admin URL")
    psql = shutil.which("psql")
    if not psql:
        parser.error("psql is required")
    try:
        environment, admin_database = local_admin_environment(os.environ["KURA_PERF_ADMIN_DATABASE_URL"])
    except ValueError as error:
        parser.error(str(error))

    database = generated_database_name()
    create_attempted = False
    exit_code = 0
    try:
        exists = run_psql(
            psql,
            admin_database,
            environment,
            sql=f"SELECT 1 FROM pg_database WHERE datname = '{database}';",
            step="check generated database name",
        )
        if exists:
            raise RuntimeError("generated database already exists; refusing to touch it")
        create_attempted = True
        run_psql(
            psql,
            admin_database,
            environment,
            sql=f"CREATE DATABASE {database} TEMPLATE template0 ENCODING 'UTF8';",
            step="create isolated database",
        )
        print(f"database={database} rows={args.rows} (synthetic metadata and media references only)")

        migration_files = sorted(MIGRATIONS.glob("[0-9][0-9][0-9]_*.sql"))
        if not migration_files:
            raise RuntimeError("no ordered database migrations found")
        for migration in migration_files:
            run_psql(psql, database, environment, file=migration, step=f"apply migration {migration.name}")
        run_psql(psql, database, environment, sql=seed_sql(args.rows), step="seed synthetic library")
        actual_count = run_psql(psql, database, environment, sql="SELECT count(*) FROM posts;", step="count synthetic posts")
        if actual_count != str(args.rows):
            raise RuntimeError("synthetic post count does not match the requested corpus size")
        for label, sql in query_cases(args.rows):
            explain_case(psql, database, environment, label, sql)
        print(
            "similarity_status=not_benchmarked (this SQL-only harness does not exercise the application similarity API)"
        )
    except (RuntimeError, OSError, json.JSONDecodeError, KeyError, TypeError) as error:
        print(f"performance harness failed: {error}", file=sys.stderr)
        exit_code = 1
    finally:
        if create_attempted:
            try:
                exists = run_psql(
                    psql,
                    admin_database,
                    environment,
                    sql=f"SELECT 1 FROM pg_database WHERE datname = '{database}';",
                    step="verify generated database before cleanup",
                )
                if exists:
                    run_psql(
                        psql,
                        admin_database,
                        environment,
                        sql=f"DROP DATABASE {database};",
                        step="drop disposable database",
                    )
                    print(f"cleaned_database={database}")
            except (RuntimeError, OSError) as error:
                print(f"cleanup failed for generated database {database}: {error}", file=sys.stderr)
                exit_code = 1
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
