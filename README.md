# Kura

An imageboard and media library built with Elm, Go + Chi, and PostgreSQL.

Elm owns browser interaction state. The Go server uses Chi to route ordinary
`net/http` handlers; domain packages own library operations, media storage,
and durable jobs. The browser and CLI share the HTTP API.

Start with the [architecture and request flow](docs/architecture.md) for the
code map, boundaries, and reasons behind the stack.

The current milestone is: **I can safely manage my actual image library here
every day.** The [daily library plan](docs/daily-library-plan.md) records the
milestone's original scope and historical acceptance checks.

- Browse a virtualized grid, open originals in Loupe, compare two posts, or survey a selection.
- Search by tags and metadata; save searches and organize posts into ordered collections.
- Edit tags, rename or merge tags across the library, revert revisions, set favorites, and score posts.
- Upload JPEG, PNG, GIF, and static WebP files, plus optional AVIF, animated WebP, MP4, and WebM support through FFmpeg; files are capped at 25 MiB.
- Keep a browser import for reload, review exact copies or visual matches, and recover mistakes through Trash.
- Share a private library with viewer, editor, and admin accounts.
- Generate thumbnails through durable PostgreSQL jobs with retries and crash recovery.
- Moderate posts in the browser with protected previews and an action history. Hidden, rejected, and deleted posts are excluded from ordinary browsing and local media access.

## Run locally

You need Go 1.27+, Elm 0.19.2, Node.js for `make web-test` and `make check`, and a running PostgreSQL database. The database commands below use `psql`, PostgreSQL's command-line client.

Create a database and set its connection URL, then run these from the repo root:

```sh
export DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/kura_v3_dev?sslmode=disable'
make db-migrate
make web-build
```

For the existing local OrbStack database, use port `55432` and the `kura` user/password.

Start the API:

```sh
make server-run
```

In another terminal, start the frontend:

```sh
make web-serve
```

This starts a Go static-file server for the built frontend on port 8000. Keep both terminals running while using the app.

Open [localhost:8000](http://localhost:8000). The frontend proxies API and media requests to `http://127.0.0.1:8080`, keeping account cookies on the same origin. Set `KURA_API_PROXY_URL` for a different API address.

Optional demo data:

```sh
make db-seed
```

The seed loads demo posts and replaces existing demo rows. Three sample rows have no matching image files and display placeholders. Migration commands do not seed data.

## Using the library

Use the moon/sun button in the top bar to switch between light and dark mode. Your choice is saved in this browser.

Click a post to select it; Cmd/Ctrl-click toggles multiple selection. Use Loupe for originals, Compare for two posts, and Survey for a small selection. Press `?` for keyboard shortcuts.

The Upload panel accepts multiple files with shared tags, source, and artist. Each upload shows its saved post and thumbnail status. **View** opens the uploaded original; returning restores the previous results and selection. Uploading does not add unrelated posts to a filtered search or collection.

**Import folder** previews up to 1,000 supported files before you start.
Hidden files, photo-library packages, and unsupported files are excluded. The
preview checks for exact duplicates; choose shared tags, artist, a source
prefix, and an optional destination collection. Import runs one image at a
time. With **Skip duplicates**, existing images can join the destination
collection without changing their metadata. Turn on **Keep this import for reload** to save the files and receipts in this browser, up to 250 MiB total. Wait for **Saved for reload**, then use **Resume saved import** after reloading. Restoring a queue never starts uploads automatically. Files whose previous outcome is uncertain need review before another attempt. Saved copies remain on this device until discarded; credentials are not saved with them. Without this option, files stay in the tab's memory only.

Edit **Source & artist** in the Inspector, then save explicitly. Clearing a
field removes its value. Metadata history records previous values, and a
conflicting edit keeps your draft until you reload the post.

Select up to 100 images to edit Source or Artist together. Choose which fields
to change, review the affected images, then save. Unchosen fields keep their
values; an empty chosen field clears it. Mixed values appear as mixed until you
choose a replacement. If any image changed elsewhere, reload the selection
before reviewing and saving again; the draft stays available.

Set **Content rating** in the Inspector to Unrated, Safe, Questionable, or
Explicit, then save. The same control applies to a selection of up to 100
images together. Existing images and new uploads start Unrated. Rating history
records changes; a conflicting edit requires a reload before another save.
Ratings describe content and do not change its visibility.

Navigator shows collection counts and offers paged ordering, rename, and delete.
First/last and neighbor moves send one post at a time. Deleting a collection
leaves its images in the library.

Use **Move selected to Trash** to review a confirmation before hiding up to 100
selected posts. Open **Trash** to restore them; its recovery list uses metadata
instead of publicly exposing hidden originals. **Export selection (ZIP)** or
**Export collection (ZIP)** downloads originals with optional metadata. Tag
suggestions appear after two characters in the Inspector's Add tags field;
choosing a suggestion edits the draft, and **Save tags** applies it.

Open **Exact copies** to review images that share identical original bytes.
Groups and their members load in pages. **Compare copies** opens the normal
Compare view and returns to the same library selection and scroll position.
Choose copies with their checkboxes, then confirm **Move chosen copies to
Trash**; nothing is chosen or removed automatically. Restore mistakes through
Trash. Hidden and trashed images stay out of the review. Similar-looking images
with different original bytes are outside this check.

Select a post and choose **Find similar** to review resized or recompressed visual matches. This uses a small perceptual fingerprint, so even distance zero can match a different image. Compare before choosing anything for Trash. New uploads are indexed automatically; **Build index for library** queues missing fingerprints for older images. Failed indexing requires an explicit retry after correcting the cause. Cropped, rotated, and semantic matches are outside this algorithm.

Admins can use **Moderation** to review Published, Pending, Hidden, or Rejected posts. Protected thumbnail previews stay available while originals are hidden. Hide/reject and bulk actions require confirmation; history records the actor and reason.

Open **Manage tags** to rename a tag or merge it into an existing tag. Preview
the affected image count, then confirm. The operation includes hidden and
trashed images and handles at most 1,000 images together. A change to tags after
the preview requires a new preview. Old tag names redirect to the current name
in exact `tag:` searches, uploads, edits, and history reverts; historical entries
keep their original names. Ordinary text and artist searches keep their meaning.

Type a query into the search bar and press Enter. Leave it blank to browse all visible posts, newest first.

Plain words search the post's text. Use `tag:` to match a specific tag. Put filters together with spaces to narrow the results:

| To find | Enter |
| --- | --- |
| Posts containing the word "cat" | `cat` |
| Posts tagged "cat" | `tag:cat` |
| Posts tagged "cat", excluding those tagged "dog" | `tag:cat -tag:dog` |
| Images you have marked safe | `rating:safe` |
| Images still awaiting classification | `rating:unrated` |
| Favorites with a score of at least 5 | `favorite:true score:>=5` |
| Images by a particular artist | `artist:"ARTIST NAME"` |
| Images imported from a folder | `source:"folder-import/FOLDER/image.png"` |
| JPEG images at least 1200 pixels wide | `media_type:image/jpeg width:>=1200` |
| Posts tagged "cat", highest score first | `tag:cat order:score` |
| The post with ID 2004 | `id:=2004` |

For numbers, `>=` means "at least", `<=` means "at most", `>` means "more than", `<` means "less than", and `=` means "exactly". For example, `width:<1200` finds images narrower than 1200 pixels. Write `score:=5` for exactly 5; `score:5` is not accepted.

## Configuration

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | PostgreSQL connection URL. |
| `HTTP_ADDR` | API listen address; default `:8080`. |
| `MEDIA_ROOT` | Local media directory; default `web/static/media` when using `make server-run`. |
| `KURA_API_TOKEN` | Require a bearer token for API writes. Unset means open local mode. |
| `KURA_API_URL` | CLI API address; default `http://localhost:8080`. |
| `KURA_API_PROXY_URL` | Frontend proxy target; default `http://127.0.0.1:8080`. |
| `KURA_PRIVATE_LIBRARY` | Set `true` to require an account or deployment bearer for library reads and writes. |
| `KURA_AUTH_ALLOW_HTTP_LOCAL` | Explicitly allow account cookies over HTTP on loopback hosts for local development. |
| `KURA_AUTH_TRUSTED_ORIGIN` | Exact public origin for cookie write checks, for example `https://library.example`. The CLI sends this origin on account-session writes when it is set. |
| `KURA_PRIVATE_READS` | CLI opt-in to send the deployment bearer on ordinary reads and ZIP exports. Requires `KURA_API_TOKEN`. |
| `KURA_BACKUP_ROOT` | Existing local backup directory shown in the admin recovery panel. |
| `KURA_BACKUP_INTERVAL` | Optional automatic backup interval, at least `1h`; unset disables scheduling and retention. |
| `KURA_BACKUP_KEEP` | Total verified primary backups to retain after a successful replica, default `7` when scheduling is enabled. |
| `KURA_BACKUP_REPLICA_ROOT` | Optional existing second local directory outside the primary backup and media roots. |
| `KURA_FFPROBE_PATH`, `KURA_FFMPEG_PATH` | Optional tool paths for extended formats; otherwise resolved from `PATH`. |
| `KURA_HTTP_TIMEOUT` | CLI timeout per HTTP request; default `10s`, accepts durations such as `30s` or `1m`. |
| `KURA_SESSION_FILE` | Optional CLI path for `kura login --persist`. The default is a file under the user config directory. |

Local originals live in `MEDIA_ROOT/uploads`; thumbnails live in `MEDIA_ROOT/derivatives`. Both runtime directories are ignored by Git at the default location. Back up the media directory together with PostgreSQL. The tracked `demo` directory contains bundled fixtures.

Set `S3_ENDPOINT` and `S3_BUCKET` to use S3-compatible storage. Additional settings are `S3_REGION` (default `us-east-1`), `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, and `S3_URL_PREFIX` for browser-facing object URLs in public mode. Public mode stores those URLs and the browser loads them directly, so bucket access controls must cover them; `/media/` checks do not apply.

Private mode may use the same S3-compatible store. It ignores `S3_URL_PREFIX` and records same-origin `/media/{key}` URLs. The API opens the configured store only after authentication and the post visibility check, using the key recorded by the upload, derivative, or import writer. It does not follow storage redirects or fetch any other URL. A private library that already contains an external media URL still refuses to start; those rows are not rewritten.

The CLI sends `KURA_API_TOKEN` on writes and protected reads such as Trash. For a private library, also set `KURA_PRIVATE_READS=true`; this uses the deployment admin identity. Legacy token mode retains **Unlock**/**Lock** in the top bar, and open local mode needs no token.

Account login from the CLI uses the same session cookie as the browser and keeps that account's role. `kura login --handle NAME` reads the password from the terminal without echo, or from one stdin line when there is no terminal. It never accepts a password flag or argument. Add `--persist` to store the session in a user-private credential file (`KURA_SESSION_FILE`, or the user config directory); otherwise the session stays in that process and is revoked when it exits. Later commands use the saved session only when no deployment token is set.

Deployment-token precedence is `--api-token`, then `KURA_API_TOKEN`, then the logged-in account. A deployment token is the admin identity and is not sent together with the account cookie. Prefer `KURA_API_TOKEN` over `--api-token` so the token is not placed on the command line. Account-session writes send `Origin` from `KURA_AUTH_TRUSTED_ORIGIN` when that exact origin is set, otherwise the origin of the API URL. In private-library mode that origin must match the server's trusted origin. Token-gated open mode still requires the deployment token for writes; an account cookie does not bypass that gate.

For a private shared library, migrate the database and bootstrap the first admin offline:

```sh
go -C server run ./cmd/kura-account bootstrap --handle owner
```

The bootstrap tool reads a 12–72 byte password from standard input, never a command argument. It refuses a second initial admin. To retain saved searches owned by an existing local identity, use `--adopt-legacy local` instead of `--handle owner`. Start the API with `KURA_PRIVATE_LIBRARY=true`, then sign in through the browser. Use HTTPS in deployment; for local HTTP only, explicitly set `KURA_AUTH_ALLOW_HTTP_LOCAL=true`. Admins manage accounts under **Accounts** in Library tools. Viewers browse; editors change library content; admins also moderate and manage users. Role, password, or disabled-state changes revoke that user's sessions.

Private mode serves media through the authenticated `/media/` handler, including objects in a configured S3-compatible bucket. It still refuses to start when a stored media URL is not a same-origin `/media/` path or an allowed static icon. OAuth and passkeys are not implemented.

The native CLI can edit metadata, recover images, and save original ZIP exports:

```sh
kura post metadata --version 0 --artist "ARTIST" 10
kura post metadata --version 1 --source "" 10
kura post trash --reason "Duplicate copy" 10 12
kura post trash-list --json --limit 60
kura post restore 10 12
kura export --ids 10,12 --metadata --output selection.zip
kura collection export --version 3 --metadata --output references.zip 7
```

Use the current `metadata_version` from `kura post show --json 10` for edits,
and the collection's current `version` for its export. A stale version fails
without retrying. Omit a metadata field to preserve it, or pass an empty value
to clear it. Trash and restore accept up to 100 IDs; the recovery list reads
one page and returns `next_cursor` for the next request. Exports stream to a
private temporary file and publish only a valid ZIP, refusing an existing
destination. Large exports may need `KURA_HTTP_TIMEOUT=1m`. Place flags before
IDs and use `--json` or `--jsonl` when scripting.

## Import local test images

For ordinary library imports, use the importer without test labels:

```sh
python3 scripts/import-media.py "SOURCE_FOLDER" --dry-run
python3 scripts/import-media.py "SOURCE_FOLDER" --tag reference --artist "ARTIST" --collection 7
```

Preview first to see supported files, duplicates, and exclusions without
uploading or writing a resume manifest. Tags, artist, source prefix, destination
collection, and duplicate policy are explicit options. `--duplicate-policy skip`
is the default; `error` stops on duplicates. Deduplication covers browse-visible
posts. The importer leaves source files unchanged and stores its resume state
in ignored `.local/import-media-manifest.json`; use `--manifest` for another
folder. Resume with the same options. An uncertain upload or collection
attachment is checked before continuing rather than blindly repeated.

For demo/testing imports with `test-data` and `folder-test` labels, use:

To add a folder of local test images without changing the source files, run:

```sh
python3 scripts/import-test-media.py "SOURCE_FOLDER" --api-url http://127.0.0.1:8080
```

The importer accepts JPEG, PNG, GIF, and static WebP files up to 25 MiB, skips hidden and Photos library content, and records each file's relative path as source metadata. It adds the `test-data` and `folder-test` tags; search for imported posts with `tag:folder-test`. With FFmpeg installed on the server, static AVIF, animated WebP, MP4 (H.264/AAC), and WebM (VP8/VP9 with Opus/Vorbis) are also supported. Videos are limited to five minutes and use JPEG poster thumbnails; Loupe provides native playback controls. Animated AVIF is unsupported. Originals keep their bytes, and basic image formats work without FFmpeg. Duplicate detection checks hashes from browse-visible posts; hidden or deleted posts are outside that API listing. If the API requires a bearer token, set `KURA_API_TOKEN` in the environment. A local resume manifest is kept at `.local/test-media-manifest.json` and is ignored by Git.

The manifest belongs to one source folder and API URL. Use a separate ignored
manifest for another import. Interrupted or ambiguous uploads are verified before
continuing; unresolved results stop the import rather than resending a file.

## CLI

Sign in as an account, then reuse that role-limited session. The password is
prompted or read from stdin; `--persist` is the only way it is written to disk:

```sh
kura login --handle editor --persist
kura search --json 'tag:cat'
kura logout
```

A deployment token still wins over that login:

```sh
KURA_API_TOKEN=... kura search --json 'tag:cat'
kura search --api-token ... --json 'tag:cat'
```

```sh
go -C cli run ./cmd/kura search --json 'tag:cat score:>=5'
go -C cli run ./cmd/kura search --all --jsonl 'tag:cat'
go -C cli run ./cmd/kura post show 2004
go -C cli run ./cmd/kura collection list
go -C cli run ./cmd/kura collection posts --all --jsonl 1
go -C cli run ./cmd/kura --help
```

Commands support `--json`, `--jsonl`, and `--api-url`. Put flags before positional arguments. Search and collection posts support `--limit`, `--cursor`, and `--all`. Without `--all`, each reads one page. With it, human and JSON Lines output stream completed pages; a later failure returns a nonzero exit status, so check the status before treating that output as complete. `--all --json` is rejected because it would buffer every page in memory; use `--all --jsonl` for streaming output.

Set a content rating using the independent `rating_version` shown by
`kura post show --json ID`:

```sh
kura post rating --version 0 10 safe
```

List tags or preview a rename or merge before applying it:

```sh
kura tag list --prefix cat --json
kura tag rename --json cats cat
kura tag merge --json kitten cat
```

Rename and merge commands preview by default. To apply, repeat the command with
`--version N`, using the `version` from that preview. A stale version fails
without changing any images. Tag lists read one page; pass the returned
`next_cursor` with `--cursor` to continue. These commands use `KURA_API_TOKEN`
when the server requires authentication.

Pagination stops on duplicate posts, repeated cursors, collection version changes, API errors, or the 1,000-page limit. It never silently restarts under a changed collection. Search pagination does not freeze a changing library. Ctrl-C cancels active requests. Writes are never automatically retried; if a write response is lost or invalid, inspect the resource before retrying. API addresses must be absolute HTTP(S) URLs without embedded credentials, query strings, or fragments; reverse-proxy path prefixes are supported and redirects are reported as errors.

Run a command with `--help` for tag, favorite, score, and collection mutations. `post get` is an alias for `post show`. Collection add requests accept at most 200 IDs. Use single-post moves for ordering; legacy whole-array reorder remains available for compatible clients.

Collection summaries avoid downloading every member ID. Single-post moves,
renames, and deletion require the version you observed; a stale version returns
a conflict. Deleting a collection removes its organization, leaving posts and
media intact:

```sh
go -C cli run ./cmd/kura collection summaries --json
go -C cli run ./cmd/kura collection move --expected-version 3 --before 12 7 10
go -C cli run ./cmd/kura collection rename --expected-version 4 7 "Reference set"
go -C cli run ./cmd/kura collection delete --expected-version 5 --yes 7
```

## Recovery and exports

Trash retains originals, tags, ratings, history, and collection membership.
Restoring a post preserves its moderation state: a previously hidden post stays
hidden. Trash listing and recovery require write access. There is no permanent
delete action.

Selection and collection exports contain original files and optional
`metadata.json`. Each export is limited to 1,000 visible posts and 250 MiB of
original bytes. Hidden or trashed posts are excluded, and collection exports
check the observed version. Original hashes and sizes are verified before the
archive is sent.

Administrators can create local backups while Kura stays online from the
**Recovery** panel. Set `KURA_BACKUP_ROOT` to an existing dedicated backup
directory outside `MEDIA_ROOT`, and set `MEDIA_ROOT` to the library's actual
directory without symlink components. The server needs Python 3.9+ and
compatible `psql` and `pg_dump` tools on `PATH`. It locates the backup script
from the repository or server directory; packaged deployments should set
`KURA_ONLINE_BACKUP_SCRIPT` to the installed `scripts/online_backup_proof.py`,
with `library-backup.py` beside it.

The panel tracks creation and cancellation, lists published backups, and
verifies their manifests and hashes. Creation uses one PostgreSQL snapshot
and copies its referenced local media while writes continue. Only one
creation runs per library across server processes. A request is complete
only after verification and atomic publication; failed or canceled attempts
remain incomplete. Creation has a 30-minute timeout. S3 backups and online
restore remain unsupported; platforms without atomic no-overwrite directory
publication refuse creation.

To opt into automatic backups, set `KURA_BACKUP_INTERVAL=24h`. The first
scheduled check waits one interval. A persisted recent backup request suppresses
a duplicate after restart or when multiple servers share the library. Recovery
also discovers running work after a full browser refresh.

With scheduling enabled, successful backups are copied to
`KURA_BACKUP_REPLICA_ROOT` when configured. Each copy is hash-verified before
atomic publication. Set `KURA_BACKUP_KEEP` to retain that many verified primary
backups, including the newest. Retention runs only after a successful matching
replica, and only removes recognized service-created backups whose primary
and replica still verify. Unknown, invalid, or symlinked entries remain alone.
A missing or failed replica suppresses retention and produces a warning while
the successfully published primary remains usable. A second directory on the
same machine is a local copy; protecting against machine loss requires placing
that copy on independent storage and an additional offsite workflow.

For the standalone offline utility, stop the API, workers, and other writers
first:

```sh
python3 scripts/library-backup.py create --offline --output /ABSOLUTE/BACKUP_PATH --media-root /ABSOLUTE/MEDIA_ROOT
python3 scripts/library-backup.py verify --input /ABSOLUTE/BACKUP_PATH
```

Restore requires an empty target database in `DATABASE_URL` and a new media
destination. The utility supports local media; configured S3 storage is outside
this backup workflow.

## Release and preflight

`make package` builds a Linux and macOS release with Go 1.27 and Elm 0.19.2.
It cross-compiles the server, frontend server, and CLI with CGO disabled,
then copies the built Elm program, CSS, backup script, and migrations into
`dist/kura-release` with `SHA256SUMS`. The archive does not seed a database
and does not include a media library.

Before starting a packaged server, run `kura-server preflight`. It checks
that `DATABASE_URL` is set, that PostgreSQL is reachable, that the `posts`
and `jobs` tables exist, that `MEDIA_ROOT` is a writable local directory,
and that `psql`, `pg_dump` 16 or newer, and `pg_restore` are on `PATH`.
An unset backup root or a missing FFmpeg is reported as disabled. Preflight
never prints the database URL, and it never migrates or seeds. Apply
migrations with `scripts/db-migrate.sh` yourself. `GET /health` stays the
liveness check; preflight is the separate readiness check.

The API server allows 30 minutes to read a request so a large upload can
finish, and it does not set a write timeout, so streamed media and ZIP
exports are not cut off. Request headers and idle connections are bounded.

## Checks

```sh
make check
go -C server test ./...
go -C server vet ./...
curl http://localhost:8080/health
```

`make check` builds the server and frontend, runs server tests and vet, tests and builds the CLI, runs the Elm suites, and checks the Python backup/import tools. Run the Elm suites directly with `make web-test` and Python tests with `make script-check`.

`make library-drill` runs API/database/media round trips using generated fixtures.
It requires `KURA_BACKUP_TEST_ADMIN_URL` pointing to an explicitly disposable
local PostgreSQL instance, plus compatible `psql`, `pg_dump`, and `pg_restore`.
The drills create and remove uniquely named databases; they never use the
development library or real source media.

PostgreSQL-backed server tests require `KURA_TEST_DATABASE_URL` pointing to a disposable database; they truncate test tables. Without that variable, those tests skip. `make check` runs server packages serially; to run the integration tests directly:

```sh
KURA_TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/kura_test?sslmode=disable' \
  go -C server test -p 1 ./...
```

## Code

The server entry point is `server/cmd/kura-server/main.go`. Its dependency
wiring leads to `server/internal/httpapi/router.go`, the Chi route and
middleware map. HTTP handlers live in that package's feature files; domain
contracts and PostgreSQL operations live in `server/internal/posts`, with
separate `auth`, `media`, and `jobs` packages.

```text
web/       Elm frontend and CSS
server/    Go API, media storage, and job worker
cli/       Command-line client
db/        PostgreSQL migrations and demo seed
scripts/   Database commands
docs/      Design and architecture notes
```

The generated Docker backup lifecycle drill runs two API processes with fresh
PostgreSQL and shared fixture storage, exercises cancellation/restart and
replication/retention, and restores a replica into empty targets. It requires
Docker, Go, network access for the disposable image, and no development secrets:

```sh
python3 scripts/backup-lifecycle-drill.py --run
```

It removes its own containers, network, volume, and acceptance image on exit.
Scheduler timing is covered by clock-controlled Go tests rather than shortening
production timer limits. The replica directory in this drill simulates another
disk and does not establish offsite durability.
