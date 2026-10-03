# Kura V3

Kura is an imageboard and media-management application. This repository is the small foundation for the V3 Elm and Go implementation.

## Initial boundaries

- **Elm** owns browser state, routing, search and media-management interaction.
- **Go** owns the HTTP API and domain behavior. The routing and database stack is chi, pgx, and handwritten SQL over PostgreSQL; generated query code is deferred until the domain contract settles.
- **PostgreSQL** will be durable structured truth. Large media will live behind an S3-compatible object-store abstraction.
- **Redis, realtime transports, Rust services, queues, and deployment infrastructure** are deliberately deferred until a concrete product requirement needs them.
- **The `kura` CLI** uses the same HTTP API as Elm. It supports `kura search` plus `kura collection list` and `kura collection posts` with human, JSON, and JSONL output.

The first product slice is a local search path into a Lightroom-style Library workspace: a virtualized MediaGrid with adjustable density, separate active post and multi-selection, Quick Look (Loupe), Compare, Survey, a Filmstrip, an Inspector, and keyboard navigation throughout (`?` lists the shortcuts), plus CLI access. The workspace design is in `docs/frontend-design.md`. Post details, optimistic tag editing with revert, favorites, scores, ordered collections, collection browsing, and a bounded KuraQL filter subset are live.

## Repository layout

```text
web/       Elm application
server/    Go HTTP server and future domain packages
cli/       CLI entry point reserved for the shared HTTP API client
db/        PostgreSQL migrations and handwritten SQL queries
scripts/   small repository scripts
docs/      architecture and domain notes
```

## Run the local slice

From the repository root:

```sh
export DATABASE_URL='postgres://USER:PASSWORD@localhost:5432/kura_v3_dev?sslmode=disable'
make db-migrate       # applies schema only; safe to run before the server
make db-seed          # explicitly loads deterministic development rows
make server-run       # listens on HTTP_ADDR (default :8080)
make web-build        # writes web/dist/elm.js
make web-serve        # serves web/ on http://localhost:8000
go -C cli run ./cmd/kura search --json "cat demo"
```

`make db-migrate` and `make db-seed` require both `DATABASE_URL` and the PostgreSQL `psql` client. `make db-setup` runs both commands for a fresh development database. Migrations never load or reset application data. The seed uses stable `/media/demo/...` URL paths served by the local fixture route described below.

The repository includes twelve small demo fixtures under `web/static/media/demo`, one copied from each supplied sample folder. They are development-only files totaling about 1.5 MB. The seed includes matching deterministic rows with the source folder name in `search_text`, so queries such as `kson` or `Ruin Explorers` return an image that the grid can load.

The Go server serves `/media/...` from `MEDIA_ROOT`. When `MEDIA_ROOT` is unset, the documented `make server-run` command uses `../web/static/media` relative to the `server` module. Set `MEDIA_ROOT` to point at another local fixture directory when needed. The route uses Go's `http.FileServer` rooted at that directory and is intended for local demo media only; it does not provide production media storage.

The health endpoint is `GET http://localhost:8080/health` and returns a small JSON status response. Search uses `GET /api/posts?q=cat` and returns post summaries from visible rows. Empty or whitespace-only queries browse the newest visible rows using the same cursor pagination. Queries longer than 256 characters return `400`; an unconfigured database returns `503` and other storage failures return `500`. Search uses PostgreSQL's forgiving web search parser, which accepts ordinary multiword queries.

The CLI uses the same endpoint and accepts `--json`, `--jsonl`, `--api-url`, `KURA_API_URL`, `--limit`, and `--cursor` for both filtered search and newest browse.

## Next milestone

The first search-to-MediaGrid slice is wired through PostgreSQL, the CLI, and local demo media. The server now validates a bounded KuraQL subset for ordinary terms, exclusions, favorite, score, width, and height filters. The next query work can add richer AST composition and sort-aware cursors before the frontend identity pass.
