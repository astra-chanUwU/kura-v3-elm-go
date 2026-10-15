#!/bin/sh
# Create/restore proof against an explicitly disposable local PostgreSQL.
# Requires psql, pg_dump, pg_restore, python3, and Go. Does not print the URL.
set -eu

if [ -z "${KURA_BACKUP_TEST_ADMIN_URL:-}" ]; then
    echo "KURA_BACKUP_TEST_ADMIN_URL is required for the backup create/restore proof" >&2
    exit 1
fi

for tool in psql pg_dump pg_restore python3 go; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "required tool is missing: $tool" >&2
        exit 1
    fi
done

dump_version=$(pg_dump --version)
major=$(printf '%s\n' "$dump_version" | sed -n 's/.* \([0-9][0-9]*\)\..*/\1/p')
if [ -z "$major" ] || [ "$major" -lt 16 ]; then
    echo "pg_dump must be PostgreSQL 16 or newer; found: $dump_version" >&2
    exit 1
fi

cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
PYTHONPATH=scripts python3 -m unittest -v test_online_backup_proof_integration test_library_backup_integration
