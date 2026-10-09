# Kura

An imageboard and media library built with Elm, Go, and PostgreSQL.

The current milestone is: **I can safely manage my actual image library here
every day.** See the [daily library plan](docs/daily-library-plan.md) for verified
status and the next browser-write and collection slices. The
[Muse slice prompts](docs/muse-slices/README.md) are ready to run sequentially
under supervising Codex review.

- Browse a virtualized grid, open originals in Loupe, compare two posts, or survey a selection.
- Search by tags and metadata; save searches and organize posts into ordered collections.
- Edit tags, revert revisions, set favorites, and score posts.
- Upload JPEG, PNG, and GIF files, up to 25 MiB each, with a sequential queue and transfer progress.
- Generate thumbnails through durable PostgreSQL jobs with retries and crash recovery.
- Moderate posts through the API. Hidden, rejected, and deleted posts are excluded from public browsing and local media access.

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

Open [localhost:8000](http://localhost:8000). The frontend connects to the API at `http://localhost:8080`; its address is set in `web/index.html`.

Optional demo data:

```sh
make db-seed
```

The seed loads demo posts and replaces existing demo rows. Three sample rows have no matching image files and display placeholders. Migration commands do not seed data.

## Using the library

Click a post to select it; Cmd/Ctrl-click toggles multiple selection. Use Loupe for originals, Compare for two posts, and Survey for a small selection. Press `?` for keyboard shortcuts.

The Upload panel accepts multiple files with shared tags, source, and artist. Each upload shows its saved post and thumbnail status. **View** opens the uploaded original; returning restores the previous results and selection. Uploading does not add unrelated posts to a filtered search or collection.

Type a query into the search bar and press Enter. Leave it blank to browse all visible posts, newest first.

Plain words search the post's text. Use `tag:` to match a specific tag. Put filters together with spaces to narrow the results:

| To find | Enter |
| --- | --- |
| Posts containing the word "cat" | `cat` |
| Posts tagged "cat" | `tag:cat` |
| Posts tagged "cat", excluding those tagged "dog" | `tag:cat -tag:dog` |
| Favorites with a score of at least 5 | `favorite:true score:>=5` |
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

Local originals live in `MEDIA_ROOT/uploads`; thumbnails live in `MEDIA_ROOT/derivatives`. Both runtime directories are ignored by Git at the default location. Back up the media directory together with PostgreSQL. The tracked `demo` directory contains bundled fixtures.

Set `S3_ENDPOINT` and `S3_BUCKET` to use S3-compatible storage. Additional settings are `S3_REGION` (default `us-east-1`), `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, and `S3_URL_PREFIX` for browser-facing object URLs. Bucket access controls must cover remote media; the API's local `/media/` visibility checks do not protect external object URLs.

The CLI sends `KURA_API_TOKEN` on writes. The browser currently has no token-entry or user-login screen, so browser uploads and edits use open local mode. There is no password, OAuth, or passkey login flow yet.

## Import local test images

To add a folder of local test images without changing the source files, run:

```sh
python3 scripts/import-test-media.py "SOURCE_FOLDER" --api-url http://127.0.0.1:8080
```

The importer accepts JPEG, PNG, and GIF files up to 25 MiB, skips hidden and Photos library content, and records each file's relative path as source metadata. It adds the `test-data` and `folder-test` tags; search for imported posts with `tag:folder-test`. WebP and video files are reported as unsupported. Duplicate detection checks hashes from browse-visible posts; hidden or deleted posts are outside that API listing. If the API requires a bearer token, set `KURA_API_TOKEN` in the environment. A local resume manifest is kept at `.local/test-media-manifest.json` and is ignored by Git.

The manifest belongs to one source folder and API URL. Use a separate ignored
manifest for another import. Interrupted or ambiguous uploads are verified before
continuing; unresolved results stop the import rather than resending a file.

## CLI

```sh
go -C cli run ./cmd/kura search --json 'tag:cat score:>=5'
go -C cli run ./cmd/kura post show 2004
go -C cli run ./cmd/kura collection list
go -C cli run ./cmd/kura collection posts 1
go -C cli run ./cmd/kura --help
```

Commands support `--json`, `--jsonl`, and `--api-url`. Search also supports `--limit` and `--cursor`. Run a command with `--help` for tag, favorite, score, and collection mutations.

## Checks

```sh
make check
go -C server test ./...
go -C server vet ./...
curl http://localhost:8080/health
```

`make check` builds the server and frontend, runs server tests and vet, tests and builds the CLI, and runs the Elm revision and upload-queue suites. Run the Elm suites directly with `make web-test`.

PostgreSQL-backed server tests require `KURA_TEST_DATABASE_URL` pointing to a disposable database; they truncate test tables. Without that variable, those tests skip. `make check` runs server packages serially; to run the integration tests directly:

```sh
KURA_TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/kura_test?sslmode=disable' \
  go -C server test -p 1 ./...
```

## Code

```text
web/       Elm frontend and CSS
server/    Go API, media storage, and job worker
cli/       Command-line client
db/        PostgreSQL migrations and demo seed
scripts/   Database commands
docs/      Design and architecture notes
```
