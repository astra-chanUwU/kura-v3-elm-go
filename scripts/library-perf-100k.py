#!/usr/bin/env python3
"""Measure Kura library SQL at 100k synthetic rows on a disposable database.

The corpus is generated metadata only: posts, tags, moderation states, trash
rows, and dhash-v1 fingerprints. It does not load images and does not turn
dhash-v1 into semantic search. Page sizes match the library UI (search,
moderation, and Trash fetch 60; similarity fetches 30 at distance 8).

Budgets are not established here. This harness records hardware, database
settings, latency, and plans. It does not add indexes or rewrite queries.
"""

import argparse
import ctypes
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import subprocess
import sys
import time
from urllib.parse import parse_qs, unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "db" / "migrations"
DATABASE_PREFIX = "kura_s7perf_"
ROWS = 100_000
SEARCH_FETCH = 61
SIMILAR_FETCH = 31
SIMILAR_DISTANCE = 8
ANCHOR_ID = 99_999
NEAR_COPY_COUNT = 240
NEAR_PER_DISTANCE = 30
TRASH_EPOCH_SQL = "timestamptz '2026-01-01 00:00:00+00'"
ADMIN_URL_ENV = "KURA_S7_ADMIN_DATABASE_URL"

SEARCH_TYPES = (
    "text", "boolean", "text", "integer", "text", "integer", "text", "integer",
    "text", "bigint", "text", "bigint", "text", "text", "text", "text[]", "text[]",
    "bigint", "text", "integer",
)
MODERATION_TYPES = ("text", "integer", "bigint")
TRASH_TYPES = ("timestamptz", "bigint", "integer")
SIMILAR_TYPES = ("bigint", "bit(64)", "integer", "integer", "bigint", "integer")

STATEMENT_SOURCES = {
    "search": (ROOT / "server" / "internal" / "posts" / "postgres_sql.go", "SearchPostsSQL"),
    "moderation": (ROOT / "server" / "internal" / "posts" / "postgres_sql.go", "moderationQueuePageSQL"),
    "trash": (ROOT / "server" / "internal" / "posts" / "trash_postgres.go", "listTrashSQL"),
    "similarity": (ROOT / "server" / "internal" / "posts" / "similarity_postgres.go", "similarCandidatesSQL"),
}


def local_admin_environment(value):
    """Convert a local PostgreSQL URL to libpq environment values, never argv."""
    try:
        parsed = urlsplit(value)
        if parsed.scheme not in ("postgres", "postgresql"):
            raise ValueError
        if not parsed.hostname or not parsed.username or not parsed.path.strip("/"):
            raise ValueError
        if parsed.hostname.lower() not in ("localhost", "127.0.0.1", "::1"):
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
        if database.startswith(DATABASE_PREFIX):
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
            "PGAPPNAME": "kura-library-perf-100k",
            "PGCLIENTENCODING": "UTF8",
        }, database
    except (TypeError, ValueError):
        raise ValueError(
            f"{ADMIN_URL_ENV} must be a local postgresql URL with a non-disposable database name"
        ) from None


def generated_database_name(token=None):
    name = DATABASE_PREFIX + (token or secrets.token_hex(12))
    assert_disposable_database(name)
    return name


def assert_disposable_database(name):
    if not re.fullmatch(r"kura_s7perf_[a-f0-9]{24}", name):
        raise ValueError("refusing to touch a database outside kura_s7perf_ plus a 24-hex token")
    return name


def moderation_state(post_id):
    remainder = post_id % 100
    if remainder == 1:
        return "pending"
    if remainder == 2:
        return "hidden"
    if remainder == 3:
        return "rejected"
    return "published"


def is_trashed(post_id):
    return post_id % 50 == 0


def is_published_visible(post_id):
    return moderation_state(post_id) == "published" and not is_trashed(post_id)


def is_rare_visible(post_id):
    return post_id % 997 == 0 and is_published_visible(post_id)


def is_batch_seven(post_id):
    return post_id % 100 == 7


def descending(predicate):
    return [post_id for post_id in range(ROWS, 0, -1) if predicate(post_id)]


def prefix_end(ids, numerator=9, denominator=10):
    """Return the last id of a newest-first prefix and how many rows remain after it."""
    prefix = len(ids) * numerator // denominator
    if prefix < 1 or prefix >= len(ids):
        raise ValueError("corpus prefix does not leave a later page")
    if prefix % 60 != 0:
        raise ValueError("deep cursor is not on a 60-row page boundary")
    return ids[prefix - 1], len(ids) - prefix


def near_copies():
    rows = []
    for index in range(NEAR_COPY_COUNT):
        rows.append((7 + index * 100, 1 + index // NEAR_PER_DISTANCE))
    return rows


def similarity_later_cursor():
    """Cursor after six full UI pages (180 rows) of the designed near-copy cluster."""
    band = [post_id for post_id, distance in near_copies() if distance == 6]
    if len(band) != NEAR_PER_DISTANCE:
        raise ValueError("distance-6 near copies are not one similarity page")
    remaining = sum(1 for _post_id, distance in near_copies() if distance > 6)
    return band[-1], 6, remaining


def sql_token(value):
    if not re.fullmatch(r"[A-Za-z0-9_]*", value):
        raise ValueError("refusing non-token SQL literal")
    return "'" + value + "'"


def search_execute_args(text, tags, cursor):
    values = ["NULL"] * 20
    values[0] = sql_token(text)
    if tags is not None:
        values[15] = "ARRAY[" + ",".join(sql_token(tag) for tag in tags) + "]::text[]"
    if cursor is not None:
        values[17] = str(int(cursor))
    values[19] = str(SEARCH_FETCH)
    return ", ".join(values)


def moderation_execute_args(cursor):
    bound = "NULL" if cursor is None else str(int(cursor))
    return f"'pending', {SEARCH_FETCH}, {bound}"


def trash_execute_args(cursor):
    if cursor is None:
        return f"NULL, NULL, {SEARCH_FETCH}"
    cursor = int(cursor)
    return f"{TRASH_EPOCH_SQL} + ({cursor} * interval '1 second'), {cursor}, {SEARCH_FETCH}"


def similarity_execute_args(cursor_distance, cursor_id):
    distance = "NULL" if cursor_distance is None else str(int(cursor_distance))
    ident = "NULL" if cursor_id is None else str(int(cursor_id))
    return (
        f"{ANCHOR_ID}, x'0000000000000000'::bit(64), {SIMILAR_DISTANCE}, "
        f"{distance}, {ident}, {SIMILAR_FETCH}"
    )


def page_rows(remaining, fetch):
    return min(int(remaining), int(fetch))


def query_cases():
    visible = descending(is_published_visible)
    search_cursor, search_remaining = prefix_end(visible)
    batch = descending(is_batch_seven)
    tag_cursor, tag_remaining = prefix_end(batch)
    rare = descending(is_rare_visible)
    if len(rare) <= 60:
        raise ValueError("rare text corpus does not fill a later page")
    rare_cursor = rare[59]
    rare_remaining = len(rare) - 60
    pending = descending(lambda post_id: moderation_state(post_id) == "pending")
    pending_cursor, pending_remaining = prefix_end(pending)
    trashed = descending(is_trashed)
    trash_cursor, trash_remaining = prefix_end(trashed)
    similar_id, similar_distance, similar_remaining = similarity_later_cursor()
    search_sql_key = "search"
    return [
        ("search_newest_first", search_sql_key, SEARCH_TYPES, search_execute_args("", None, None), SEARCH_FETCH),
        ("search_newest_later", search_sql_key, SEARCH_TYPES, search_execute_args("", None, search_cursor), page_rows(search_remaining, SEARCH_FETCH)),
        ("search_tag_first", search_sql_key, SEARCH_TYPES, search_execute_args("", ["batch_7"], None), SEARCH_FETCH),
        ("search_tag_later", search_sql_key, SEARCH_TYPES, search_execute_args("", ["batch_7"], tag_cursor), page_rows(tag_remaining, SEARCH_FETCH)),
        ("search_text_rare_first", search_sql_key, SEARCH_TYPES, search_execute_args("rareanchor", None, None), SEARCH_FETCH),
        ("search_text_rare_later", search_sql_key, SEARCH_TYPES, search_execute_args("rareanchor", None, rare_cursor), page_rows(rare_remaining, SEARCH_FETCH)),
        ("moderation_pending_first", "moderation", MODERATION_TYPES, moderation_execute_args(None), SEARCH_FETCH),
        ("moderation_pending_later", "moderation", MODERATION_TYPES, moderation_execute_args(pending_cursor), page_rows(pending_remaining, SEARCH_FETCH)),
        ("trash_first", "trash", TRASH_TYPES, trash_execute_args(None), SEARCH_FETCH),
        ("trash_later", "trash", TRASH_TYPES, trash_execute_args(trash_cursor), page_rows(trash_remaining, SEARCH_FETCH)),
        ("similarity_first", "similarity", SIMILAR_TYPES, similarity_execute_args(None, None), SIMILAR_FETCH),
        ("similarity_later", "similarity", SIMILAR_TYPES, similarity_execute_args(similar_distance, similar_id), page_rows(similar_remaining, SIMILAR_FETCH)),
    ]


def extract_go_const(path, name):
    text = path.read_text(encoding="utf-8")
    match = re.search(rf"const {re.escape(name)} = `([^`]*)`", text)
    if not match:
        raise RuntimeError(f"missing SQL const {name}")
    statement = match.group(1).strip()
    if not statement or ";" in statement:
        raise RuntimeError(f"SQL const {name} is empty or contains a semicolon")
    return statement


def load_statements():
    return {key: extract_go_const(path, name) for key, (path, name) in STATEMENT_SOURCES.items()}


def seed_sql():
    return f"""\
INSERT INTO posts (
    id, preview_url, original_url, media_type, width, height, search_text,
    source, artist, hash, file_size, tags, moderation_state, deleted_at, created_at
)
SELECT
    g,
    '/synthetic/media/' || g::text || '.jpg',
    '/synthetic/media/' || g::text || '.jpg',
    'image/jpeg',
    1280 + (g % 5) * 120,
    720 + (g % 7) * 90,
    CASE WHEN g % 997 = 0 THEN 'rareanchor synthetic subject' ELSE 'synthetic subject' END
        || ' batchtoken' || (g % 1000)::text,
    'synthetic',
    'synthetic',
    'sha256:' || lpad(to_hex(g), 64, '0'),
    1024 + (g % 500000),
    ARRAY['synthetic', 'batch_' || (g % 100)::text]
        || CASE WHEN g % 997 = 0 THEN ARRAY['rareanchor'] ELSE ARRAY[]::text[] END,
    CASE
        WHEN g % 100 = 1 THEN 'pending'
        WHEN g % 100 = 2 THEN 'hidden'
        WHEN g % 100 = 3 THEN 'rejected'
        ELSE 'published'
    END,
    CASE
        WHEN g % 50 = 0 THEN {TRASH_EPOCH_SQL} + (g * interval '1 second')
        ELSE NULL
    END,
    timestamptz '2024-01-01 00:00:00+00' + (g * interval '1 minute')
FROM generate_series(1, {ROWS}) AS generated(g);

SELECT setval(pg_get_serial_sequence('posts', 'id'), {ROWS}, true);

INSERT INTO post_trash_actions (post_id, action, actor, reason)
SELECT id, 'trash', 'synthetic', 'slice7'
FROM posts
WHERE deleted_at IS NOT NULL;

INSERT INTO post_moderation_actions (post_id, action, previous_state, new_state, actor, reason)
SELECT
    id,
    CASE moderation_state
        WHEN 'pending' THEN 'pend'
        WHEN 'hidden' THEN 'hide'
        WHEN 'rejected' THEN 'reject'
    END,
    'published',
    moderation_state,
    'synthetic',
    'slice7'
FROM posts
WHERE moderation_state <> 'published';

INSERT INTO similarity_fingerprints (post_id, algorithm, fingerprint, original_hash)
SELECT
    id,
    'dhash-v1',
    CASE
        WHEN id = {ANCHOR_ID} THEN x'0000000000000000'::bit(64)
        WHEN id % 100 = 7 AND id BETWEEN 7 AND 23907 THEN
            ((1::bigint << ((1 + ((id - 7) / 100) / 30)::integer)) - 1)::bit(64)
        ELSE
            (x'800000000000FFFF'::bit(64) | (((id::bigint)::bit(64)) << 16))
    END,
    hash
FROM posts;
"""


def redact(text):
    text = re.sub(r"postgres(?:ql)?://\S+", "[redacted-url]", text, flags=re.IGNORECASE)
    text = re.sub(r"\b\d{1,3}(?:\.\d{1,3}){3}\b", "[redacted-host]", text)
    text = re.sub(r"port \d+", "port [redacted]", text, flags=re.IGNORECASE)
    return text


def psql_child_environment(admin_environment):
    environment = os.environ.copy()
    for key in (
        "DATABASE_URL",
        "PGDATABASE",
        "PGOPTIONS",
        "PGSERVICE",
        "PGSERVICEFILE",
        ADMIN_URL_ENV,
        "KURA_PERF_ADMIN_DATABASE_URL",
    ):
        environment.pop(key, None)
    environment.update(admin_environment)
    return environment


def run_psql(psql, database, environment, sql=None, file=None, step="database operation", timeout=180, tuples_only=False):
    assert_safe_database_argument(database)
    command = [psql, "-X", "-q", "-v", "ON_ERROR_STOP=1", "-P", "pager=off", "--dbname", database]
    if tuples_only:
        command.extend(["-A", "-t"])
    if file is not None:
        command.extend(["--file", str(file)])
    try:
        result = subprocess.run(
            command,
            input=sql,
            text=True,
            encoding="utf-8",
            errors="replace",
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=environment,
            timeout=timeout,
            check=False,
        )
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"{step} exceeded {timeout} seconds") from None
    if result.returncode != 0:
        detail = [line for line in redact(result.stderr).strip().splitlines() if line.strip()]
        summary = " | ".join(detail[-4:]) if detail else "no server message"
        raise RuntimeError(f"{step} failed (psql exit {result.returncode}; {summary})")
    return result.stdout.strip()


def assert_safe_database_argument(database):
    if not re.fullmatch(r"[A-Za-z0-9_]+", database):
        raise ValueError("refusing unsafe database name")


def migration_files():
    files = sorted(MIGRATIONS.glob("[0-9][0-9][0-9]_*.sql"))
    if not files:
        raise RuntimeError("no ordered database migrations found")
    script = ["SET client_min_messages TO warning;"]
    for migration in files:
        rendered = str(migration).replace("\\", "/")
        if re.search(r"[\s'\"]", rendered):
            raise RuntimeError("migration path cannot be included safely")
        script.append(f"\\i {rendered}")
    return "\n".join(script) + "\n"


def memory_bytes():
    if sys.platform != "win32":
        return None, None
    class MemoryStatus(ctypes.Structure):
        _fields_ = [
            ("dwLength", ctypes.c_ulong),
            ("dwMemoryLoad", ctypes.c_ulong),
            ("ullTotalPhys", ctypes.c_ulonglong),
            ("ullAvailPhys", ctypes.c_ulonglong),
            ("ullTotalPageFile", ctypes.c_ulonglong),
            ("ullAvailPageFile", ctypes.c_ulonglong),
            ("ullTotalVirtual", ctypes.c_ulonglong),
            ("ullAvailVirtual", ctypes.c_ulonglong),
            ("ullAvailExtendedVirtual", ctypes.c_ulonglong),
        ]
    status = MemoryStatus()
    status.dwLength = ctypes.sizeof(MemoryStatus)
    if not ctypes.windll.kernel32.GlobalMemoryStatusEx(ctypes.byref(status)):
        return None, None
    return int(status.ullTotalPhys), int(status.ullAvailPhys)


def hardware_lines():
    total, available = memory_bytes()
    lines = [
        f"hardware.system={platform.system()}",
        f"hardware.release={platform.release()}",
        f"hardware.version={platform.version()}",
        f"hardware.machine={platform.machine()}",
        f"hardware.processor={platform.processor() or os.environ.get('PROCESSOR_IDENTIFIER', '')}",
        f"hardware.cpu_count={os.cpu_count()}",
        f"hardware.ram_total_bytes={'' if total is None else total}",
        f"hardware.ram_available_bytes={'' if available is None else available}",
        f"harness.python={platform.python_version()}",
    ]
    return lines


def source_revision():
    try:
        result = subprocess.run(
            ["git", "-C", str(ROOT), "rev-parse", "HEAD"],
            text=True,
            encoding="utf-8",
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=15,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return "unavailable"
    if result.returncode != 0:
        return "unavailable"
    revision = result.stdout.strip()
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        return "unavailable"
    return revision


def emit(line):
    print(line, flush=True)


def parse_explain(raw):
    start = raw.find("[")
    if start < 0:
        raise RuntimeError("explain output did not contain a JSON plan")
    return json.loads(raw[start:])


def walk_plan(node, seq_scans, indexes, nodes, sorts):
    node_type = node.get("Node Type", "")
    nodes.append(node_type)
    if node_type in {"Seq Scan", "Parallel Seq Scan"}:
        seq_scans.append(node.get("Relation Name") or "")
    if "Sort" in node_type:
        sorts.append(node_type)
    if node.get("Index Name"):
        indexes.append(node["Index Name"])
    for child in node.get("Plans") or []:
        walk_plan(child, seq_scans, indexes, nodes, sorts)


def unique(items):
    seen = []
    for item in items:
        if item not in seen:
            seen.append(item)
    return seen


def buffer_totals(node):
    hits = int(node.get("Shared Hit Blocks") or 0)
    reads = int(node.get("Shared Read Blocks") or 0)
    for child in node.get("Plans") or []:
        child_hits, child_reads = buffer_totals(child)
        hits += child_hits
        reads += child_reads
    return hits, reads


def summarize_explain(report):
    plan = report[0]["Plan"]
    seq_scans, indexes, nodes, sorts = [], [], [], []
    walk_plan(plan, seq_scans, indexes, nodes, sorts)
    hits, reads = buffer_totals(plan)
    return {
        "rows": int(plan.get("Actual Rows", 0)),
        "planning_ms": float(report[0].get("Planning Time", 0)),
        "execution_ms": float(report[0]["Execution Time"]),
        "shared_hit_blocks": hits,
        "shared_read_blocks": reads,
        "seq_scans": ",".join(unique(seq_scans)) or "none",
        "indexes": ",".join(unique(indexes)) or "none",
        "nodes": ",".join(unique(nodes)),
        "sorts": ",".join(unique(sorts)) or "none",
    }


def measure_script(statements, cases):
    parts = [
        "SET TIME ZONE 'UTC';",
        "SET plan_cache_mode = force_custom_plan;",
    ]
    prepared = set()
    for label, key, arg_types, execute_args, _expected_rows in cases:
        if key not in prepared:
            signature = ", ".join(arg_types)
            parts.append(f"PREPARE kura_s7_{key}({signature}) AS\n{statements[key]}\n;")
            prepared.add(key)
        for run in (1, 2):
            parts.append(f"SELECT 'KURA_S7_CASE {label} {run}';")
            parts.append(
                "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)\n"
                f"EXECUTE kura_s7_{key}({execute_args});"
            )
    return "\n".join(parts) + "\n"


def measure_cases(psql, database, environment, statements, cases):
    started = time.perf_counter()
    raw = run_psql(
        psql,
        database,
        environment,
        sql=measure_script(statements, cases),
        step="measure library pages",
        timeout=600,
        tuples_only=True,
    )
    wall_ms = (time.perf_counter() - started) * 1000
    emit(f"measure_client_wall_ms={wall_ms:.3f}")
    expected = {(label, run): rows for label, _key, _types, _args, rows in cases for run in (1, 2)}
    seen = set()
    current = None
    chunk = []

    def finish():
        if current is None:
            return
        label, run = current
        summary = summarize_explain(parse_explain("\n".join(chunk)))
        if summary["rows"] != expected[(label, run)]:
            raise RuntimeError(
                f"{label} run {run} returned {summary['rows']} rows, expected {expected[(label, run)]}"
            )
        emit(
            f"case={label} run={run} rows={summary['rows']} planning_ms={summary['planning_ms']:.3f} "
            f"execution_ms={summary['execution_ms']:.3f} "
            f"shared_hit_blocks={summary['shared_hit_blocks']} shared_read_blocks={summary['shared_read_blocks']} "
            f"seq_scans={summary['seq_scans']} indexes={summary['indexes']} sorts={summary['sorts']} "
            f"nodes={summary['nodes']}"
        )
        seen.add(current)

    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        if line.startswith("KURA_S7_CASE "):
            finish()
            _marker, label, run_text = line.split(" ", 2)
            current = (label, int(run_text))
            chunk = []
            continue
        if current is not None:
            chunk.append(line)
    finish()
    missing = [f"{label} run {run}" for label, run in expected if (label, run) not in seen]
    if missing:
        raise RuntimeError("missing measurements for " + ", ".join(missing))


def verify_corpus(psql, database, environment):
    counts = run_psql(
        psql,
        database,
        environment,
        sql="""
SELECT
    (SELECT count(*) FROM posts) || '|' ||
    (SELECT count(*) FROM posts WHERE deleted_at IS NULL AND moderation_state = 'published') || '|' ||
    (SELECT count(*) FROM posts WHERE deleted_at IS NULL AND moderation_state = 'pending') || '|' ||
    (SELECT count(*) FROM posts WHERE deleted_at IS NULL AND moderation_state = 'hidden') || '|' ||
    (SELECT count(*) FROM posts WHERE deleted_at IS NULL AND moderation_state = 'rejected') || '|' ||
    (SELECT count(*) FROM posts WHERE deleted_at IS NOT NULL) || '|' ||
    (SELECT count(*) FROM post_trash_actions) || '|' ||
    (SELECT count(*) FROM post_moderation_actions) || '|' ||
    (SELECT count(*) FROM similarity_fingerprints WHERE algorithm = 'dhash-v1') || '|' ||
    (SELECT count(*) FROM posts WHERE tags @> ARRAY['batch_7'] AND deleted_at IS NULL AND moderation_state = 'published') || '|' ||
    (SELECT count(*) FROM posts WHERE search_document @@ websearch_to_tsquery('simple', 'rareanchor') AND deleted_at IS NULL AND moderation_state = 'published') || '|' ||
    (SELECT bit_count(fingerprint) FROM similarity_fingerprints WHERE post_id = 99999) || '|' ||
    (SELECT count(*) FROM similarity_fingerprints WHERE post_id % 100 = 7 AND post_id BETWEEN 7 AND 23907) || '|' ||
    (SELECT min(bit_count(fingerprint)) FROM similarity_fingerprints WHERE post_id % 100 = 7 AND post_id BETWEEN 7 AND 23907) || '|' ||
    (SELECT max(bit_count(fingerprint)) FROM similarity_fingerprints WHERE post_id % 100 = 7 AND post_id BETWEEN 7 AND 23907) || '|' ||
    (SELECT min(bit_count(fingerprint)) FROM similarity_fingerprints WHERE post_id <> 99999 AND NOT (post_id % 100 = 7 AND post_id BETWEEN 7 AND 23907));
""",
        step="verify synthetic corpus",
        timeout=180,
        tuples_only=True,
    )
    fields = [int(part) for part in counts.split("|")]
    expected = {
        "posts": ROWS,
        "published_visible": 95_000,
        "pending": 1_000,
        "hidden": 1_000,
        "rejected": 1_000,
        "trash": 2_000,
        "trash_actions": 2_000,
        "moderation_actions": 3_000,
        "fingerprints": ROWS,
        "batch_7_visible": 1_000,
        "rare_visible": len(descending(is_rare_visible)),
        "anchor_distance": 0,
        "near_copies": NEAR_COPY_COUNT,
        "near_min_distance": 1,
        "near_max_distance": 8,
    }
    names = list(expected)
    if len(fields) != len(names) + 1:
        raise RuntimeError("corpus verification returned an unexpected field count")
    for name, actual in zip(names, fields):
        if actual != expected[name]:
            raise RuntimeError(f"corpus {name}={actual}, expected {expected[name]}")
        emit(f"corpus.{name}={actual}")
    far_min = fields[-1]
    emit(f"corpus.far_min_distance={far_min}")
    if far_min < 17:
        raise RuntimeError(f"far fingerprints overlap the similarity radius (min distance {far_min})")


def record_database_conditions(psql, database, environment, admin_environment):
    emit(f"postgres.host={admin_environment['PGHOST']}")
    emit(f"postgres.port={admin_environment['PGPORT']}")
    emit(f"postgres.user={admin_environment['PGUSER']}")
    emit(f"postgres.sslmode={admin_environment['PGSSLMODE']}")
    emit("postgres.auth=trust")
    emit("postgres.plan_cache_mode=force_custom_plan")
    emit("postgres.session_time_zone=UTC")
    settings = run_psql(
        psql,
        database,
        environment,
        sql="""
SELECT name || '=' || COALESCE(setting, '') || COALESCE(' ' || NULLIF(unit, ''), '')
FROM pg_settings
WHERE name IN (
    'server_version', 'server_encoding', 'lc_collate', 'lc_ctype', 'TimeZone',
    'shared_buffers', 'work_mem', 'maintenance_work_mem', 'effective_cache_size',
    'random_page_cost', 'seq_page_cost', 'max_parallel_workers_per_gather',
    'max_parallel_workers', 'jit', 'max_connections'
)
ORDER BY name;
""",
        step="read database settings",
        tuples_only=True,
    )
    for line in settings.splitlines():
        emit(f"postgres.{line.strip()}")
    sizes = run_psql(
        psql,
        database,
        environment,
        sql="""
SELECT
    pg_database_size(current_database())::text || '|' ||
    pg_total_relation_size('posts')::text || '|' ||
    pg_total_relation_size('similarity_fingerprints')::text || '|' ||
    pg_total_relation_size('post_trash_actions')::text || '|' ||
    pg_total_relation_size('post_moderation_actions')::text;
""",
        step="read relation sizes",
        tuples_only=True,
    )
    database_bytes, posts_bytes, fingerprint_bytes, trash_bytes, moderation_bytes = sizes.split("|")
    emit(f"postgres.database_bytes={database_bytes}")
    emit(f"postgres.posts_bytes={posts_bytes}")
    emit(f"postgres.similarity_fingerprints_bytes={fingerprint_bytes}")
    emit(f"postgres.post_trash_actions_bytes={trash_bytes}")
    emit(f"postgres.post_moderation_actions_bytes={moderation_bytes}")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true", help="create, measure, and drop a disposable local database")
    args = parser.parse_args(argv)
    if not args.run:
        parser.error("--run is required; this tool creates and drops a disposable database")
    if not os.environ.get(ADMIN_URL_ENV):
        parser.error(f"set {ADMIN_URL_ENV} to an explicit local PostgreSQL admin URL")
    psql = shutil.which("psql")
    if not psql:
        parser.error("psql is required")
    try:
        admin_environment, admin_database = local_admin_environment(os.environ[ADMIN_URL_ENV])
    except ValueError as error:
        parser.error(str(error))
    environment = psql_child_environment(admin_environment)
    database = generated_database_name()
    create_attempted = False
    exit_code = 0
    try:
        statements = load_statements()
        cases = query_cases()
        for line in hardware_lines():
            emit(line)
        emit(f"source_revision={source_revision()}")
        emit("budget=not_established")
        emit("query_changes=none")
        emit("index_changes=none")
        emit(f"corpus.rows={ROWS}")
        emit("corpus.media=synthetic-metadata-only")
        emit("similarity.algorithm=dhash-v1")
        exists = run_psql(
            psql,
            admin_database,
            environment,
            sql=f"SELECT 1 FROM pg_database WHERE datname = '{database}';",
            step="check generated database name",
            tuples_only=True,
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
        emit(f"database={database}")
        run_psql(psql, database, environment, sql=migration_files(), step="apply migrations", timeout=180)
        emit("phase=seed")
        run_psql(psql, database, environment, sql=seed_sql(), step="seed synthetic library", timeout=300)
        emit("phase=verify")
        verify_corpus(psql, database, environment)
        emit("phase=vacuum-analyze")
        run_psql(
            psql,
            database,
            environment,
            sql="""
VACUUM (ANALYZE) posts;
VACUUM (ANALYZE) similarity_fingerprints;
VACUUM (ANALYZE) post_trash_actions;
VACUUM (ANALYZE) post_moderation_actions;
""",
            step="vacuum analyze measured tables",
            timeout=300,
        )
        emit("maintenance=VACUUM ANALYZE posts, similarity_fingerprints, post_trash_actions, post_moderation_actions")
        record_database_conditions(psql, database, environment, admin_environment)
        emit("phase=measure")
        emit("latency=explain_analyze_execution_ms")
        emit("runs=2")
        measure_cases(psql, database, environment, statements, cases)
        emit("skips=none")
        emit("improvement_claimed=no")
    except (RuntimeError, OSError, json.JSONDecodeError, KeyError, TypeError, ValueError) as error:
        emit(f"performance harness failed: {redact(str(error))}")
        exit_code = 1
    finally:
        if create_attempted:
            try:
                assert_disposable_database(database)
                exists = run_psql(
                    psql,
                    admin_database,
                    environment,
                    sql=f"SELECT 1 FROM pg_database WHERE datname = '{database}';",
                    step="verify generated database before cleanup",
                    tuples_only=True,
                )
                if exists:
                    run_psql(
                        psql,
                        admin_database,
                        environment,
                        sql=f"DROP DATABASE {database} WITH (FORCE);",
                        step="drop disposable database",
                    )
                    emit(f"cleaned_database={database}")
            except (RuntimeError, OSError, ValueError) as error:
                emit(f"cleanup failed for generated database {database}: {redact(str(error))}")
                exit_code = 1
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
