#!/bin/sh
# Build a reproducible Linux/macOS release. Elm output is portable;
# the Go binaries are cross-compiled with CGO disabled.
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

out="$root/dist/kura-release"
rm -rf "$out"
mkdir -p "$out/bin" "$out/web/dist" "$out/web/static/icons" "$out/scripts" "$out/db/migrations"

build_one() {
    os_name=$1
    arch=$2
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C server build -trimpath -o "$out/bin/kura-server-${os_name}-${arch}" ./cmd/kura-server
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C server build -trimpath -o "$out/bin/kura-web-${os_name}-${arch}" ./cmd/kura-web
    CGO_ENABLED=0 GOOS="$os_name" GOARCH="$arch" go -C cli build -trimpath -o "$out/bin/kura-${os_name}-${arch}" ./cmd/kura
}

build_one linux amd64
build_one darwin amd64
build_one darwin arm64

( cd web && elm make src/Main.elm --output=dist/elm.js )
cp web/index.html web/kura.css web/browser-import.js web/moderation-preview.js "$out/web/"
cp web/dist/elm.js "$out/web/dist/elm.js"
cp web/static/icons/*.svg "$out/web/static/icons/"
cp scripts/library-backup.py scripts/db-migrate.sh "$out/scripts/"
cp db/migrations/*.sql "$out/db/migrations/"

cat > "$out/RELEASE.txt" << 'EOF'
Kura release

Toolchains: Go 1.27 and Elm 0.19.2. Binaries are linux/amd64, darwin/amd64,
and darwin/arm64, built with CGO disabled. The Elm program is the same
JavaScript on every platform.

Startup
1. Install PostgreSQL 16 or newer, plus psql, pg_dump, and pg_restore.
   FFmpeg is optional and only needed for extended media.
2. Put DATABASE_URL, MEDIA_ROOT, and HTTP_ADDR in the server environment.
   Do not pass the database URL or tokens on the command line.
3. Create an empty database. Apply db/migrations with scripts/db-migrate.sh.
   Do not run a seed against a library that already has data.
4. Run: kura-server preflight
   Required checks must pass. A disabled FFmpeg line is expected when
   extended media is unused.
5. Start kura-server. Liveness is GET /health. Preflight is the separate
   readiness check.

Upgrade
Stop the server. Keep the existing database and MEDIA_ROOT. Replace the
binaries and web assets. Run scripts/db-migrate.sh again; the migrations
are additive and do not delete data. Run preflight, then start the server.
Do not seed, and do not delete the database or media directory.

Backups
Scheduled backups stay inside the server and remain disabled until
KURA_BACKUP_INTERVAL is set to at least 1h. scripts/library-backup.py is
the offline operator tool. Restore with that script only into an empty
database after every writer has stopped.

This archive does not contain a development database, media library, or secrets.
EOF

python3 - "$out" << 'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
lines = []
for path in sorted(p for p in root.rglob("*") if p.is_file() and p.name != "SHA256SUMS"):
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    lines.append(f"{digest}  {path.relative_to(root).as_posix()}")
(root / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")
PY

echo "release written to dist/kura-release"
