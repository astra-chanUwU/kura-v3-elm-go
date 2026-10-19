#!/bin/sh
# Build a release for the host running this script. Elm output is portable.
# Go binaries are built for this host only, with CGO disabled.
# Run the script on another operating system before claiming that platform.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

goversion=$(go env GOVERSION)
case "$goversion" in
    go1.27.*) ;;
    *)
        echo "Go 1.27 is required; found ${goversion}" >&2
        exit 1
        ;;
esac
elmversion=$(elm --version)
if [ "$elmversion" != "0.19.2" ]; then
    echo "Elm 0.19.2 is required" >&2
    exit 1
fi

host_os=$(go env GOOS)
host_arch=$(go env GOARCH)
suffix=""
if [ "$host_os" = "windows" ]; then
    suffix=".exe"
fi

out="$root/dist/kura-release"
rm -rf "$out"
mkdir -p "$out/bin" "$out/web/dist" "$out/web/static/icons" "$out/web/static/brand" "$out/scripts" "$out/db/migrations"

build_one() {
    os_name=$1
    arch=$2
    bin_suffix=$3
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C server build -trimpath -o "$out/bin/kura-server-${os_name}-${arch}${bin_suffix}" ./cmd/kura-server
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C server build -trimpath -o "$out/bin/kura-web-${os_name}-${arch}${bin_suffix}" ./cmd/kura-web
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C server build -trimpath -o "$out/bin/kura-account-${os_name}-${arch}${bin_suffix}" ./cmd/kura-account
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C cli build -trimpath -o "$out/bin/kura-${os_name}-${arch}${bin_suffix}" ./cmd/kura
}

build_one "$host_os" "$host_arch" "$suffix"

( cd web && elm make src/Main.elm --output=dist/elm.js )
cp web/index.html web/kura.css web/browser-import.js web/moderation-preview.js "$out/web/"
cp web/dist/elm.js "$out/web/dist/elm.js"
cp web/static/icons/*.svg "$out/web/static/icons/"
cp web/static/brand/auth-observatory.png "$out/web/static/brand/"
cp scripts/library-backup.py scripts/online_backup_proof.py scripts/db-migrate.sh "$out/scripts/"
cp db/migrations/*.sql "$out/db/migrations/"

cat > "$out/RELEASE.txt" << EOF
Kura release

Toolchains: Go 1.27 and Elm 0.19.2. This archive contains one host target:
${host_os}/${host_arch}, built with CGO disabled. Other platforms are not
included. The Elm program is plain JavaScript.

Startup
1. Install PostgreSQL 16 or newer, plus Python 3.9 or newer, psql, pg_dump,
   and pg_restore. FFmpeg is optional and only needed for extended media.
2. Put DATABASE_URL, MEDIA_ROOT, and HTTP_ADDR in the server environment.
   Do not pass the database URL or tokens on the command line.
3. Create an empty database. Apply db/migrations with scripts/db-migrate.sh.
   The script reapplies additive SQL and is safe when those migrations are
   already present. Do not run a seed against a library that already has data.
4. Run: bin/kura-server-${host_os}-${host_arch}${suffix} preflight
   Required checks must pass. A disabled FFmpeg line is expected when
   extended media is unused. Preflight does not migrate or seed.
5. Start bin/kura-server-${host_os}-${host_arch}${suffix}. Liveness is
   GET /health and does not contact PostgreSQL. Preflight is the separate
   readiness check.
6. Start bin/kura-web-${host_os}-${host_arch}${suffix}. It serves the web
   directory packaged beside bin. Set KURA_WEB_ROOT only to override that.

Upgrade
Stop the server. Keep the existing database and MEDIA_ROOT. Replace the
binaries, web assets, scripts, and db/migrations. Run scripts/db-migrate.sh
again. Run preflight, then start the server. Do not seed, and do not delete
the database or media directory.

Backups
Scheduled backups stay inside the server and remain disabled until
KURA_BACKUP_INTERVAL is set to at least 1h. The server finds
scripts/online_backup_proof.py beside bin, and library-backup.py beside
that script, without a source checkout. Set KURA_ONLINE_BACKUP_SCRIPT only
to override that path. Restore with scripts/library-backup.py only into an
empty database after every writer has stopped.

Account maintenance
bin/kura-account-${host_os}-${host_arch}${suffix} bootstraps the first admin
or resets one existing password. It reads the password from standard input.

This archive does not contain a development database, media library, or secrets.
EOF

if command -v python3 >/dev/null 2>&1; then
    py=python3
elif command -v python >/dev/null 2>&1; then
    py=python
else
    echo "Python 3.9 or newer is required to write checksums" >&2
    exit 1
fi

"$py" - "$out" << 'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
lines = []
for path in sorted(p for p in root.rglob("*") if p.is_file() and p.name != "SHA256SUMS"):
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    lines.append(f"{digest}  {path.relative_to(root).as_posix()}")
(root / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")
PY

echo "release written to dist/kura-release (${host_os}/${host_arch})"
