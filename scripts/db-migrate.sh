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
for migration in "$repo_root"/db/migrations/*.sql; do
    [ -f "$migration" ] || continue
    case "$migration" in
        *.gitkeep) continue ;;
    esac
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$migration"
done
