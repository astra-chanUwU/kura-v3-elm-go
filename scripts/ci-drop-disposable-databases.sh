#!/bin/sh
# Drop only disposable Kura databases on a local PostgreSQL. Refuses any
# other host. Connection settings come from PGHOST, PGPORT, PGUSER,
# PGPASSWORD, and PGDATABASE; the URL is not accepted or printed.
set -eu

host=${PGHOST:-}
case "$host" in
    127.0.0.1|localhost|::1) ;;
    *)
        echo "refusing to drop databases on a non-local PostgreSQL host" >&2
        exit 1
        ;;
esac

names=$(psql -v ON_ERROR_STOP=1 -Atc "SELECT datname FROM pg_database WHERE datname ~ '^kura_[a-z0-9_]+$' AND datname <> 'kura_ci'")
for name in $names; do
    case "$name" in
        kura_[a-z0-9_]*)
            psql -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS ${name} WITH (FORCE)"
            ;;
        *)
            echo "refusing unexpected database name" >&2
            exit 1
            ;;
    esac
done
