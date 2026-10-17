#!/bin/sh
# Run the server test suite. With --require-database, an explicit
# KURA_TEST_DATABASE_URL is mandatory and a skipped PostgreSQL test fails
# the run. The URL is never printed.
set -eu

require=0
if [ "${1:-}" = "--require-database" ]; then
    require=1
    shift
fi

if [ "$require" -eq 1 ] && [ -z "${KURA_TEST_DATABASE_URL:-}" ]; then
    echo "KURA_TEST_DATABASE_URL is required; PostgreSQL-backed tests must not skip" >&2
    exit 1
fi

cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

log=$(mktemp)
trap 'rm -f "$log"' EXIT

set +e
go -C server test "$@" ./... >"$log" 2>&1
status=$?
set -e
cat "$log"

if [ "$require" -eq 1 ] || [ -n "${KURA_TEST_DATABASE_URL:-}" ]; then
    if grep -E "KURA_TEST_DATABASE_URL not set" "$log" >/dev/null; then
        echo "PostgreSQL-backed tests skipped" >&2
        exit 1
    fi
fi

exit "$status"
