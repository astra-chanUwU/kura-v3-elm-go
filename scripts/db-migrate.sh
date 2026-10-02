#!/bin/sh
set -eu

if [ -z "${DATABASE_URL:-}" ]; then
    echo "DATABASE_URL is required (example: postgres://USER:PASSWORD@localhost:5432/kura_v3_dev?sslmode=disable)" >&2
    exit 1
fi
if ! command -v psql >/dev/null 2>&1; then
    echo "psql is required to run database migrations" >&2
    exit 1
fi

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$repo_root/db/migrations/001_posts.sql"
