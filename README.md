# Kura V3

Kura is an imageboard and media-management application. This repository is the small foundation for the V3 Elm and Go implementation.

## Initial boundaries

- **Elm** owns browser state, routing, search and media-management interaction.
- **Go** owns the HTTP API and domain behavior. The routing and database stack is chi, pgx, and handwritten SQL over PostgreSQL; generated query code is deferred until the domain contract settles.
- **PostgreSQL** will be durable structured truth. Large media will live behind an S3-compatible object-store abstraction.
- **Redis, realtime transports, Rust services, queues, and deployment infrastructure** are deliberately deferred until a concrete product requirement needs them.
- **The `kura` CLI** uses the same HTTP API as Elm. It currently supports `kura search` with human, JSON, and JSONL output.

The first product slice is now a local search-to-MediaGrid path with selection, quick look, query editing, and CLI access. Frontend identity and tag editing remain later slices.

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

`make db-migrate` and `make db-seed` require both `DATABASE_URL` and the PostgreSQL `psql` client. `make db-setup` runs both commands for a fresh development database. Migrations never load or reset application data. The seed uses stable `/media/demo/...` URL paths; media fixture files and routes are not included yet, so the rows exercise search results but do not render actual images.

The health endpoint is `GET http://localhost:8080/health` and returns a small JSON status response. Search uses `GET /api/posts?q=cat` and returns post summaries from visible rows. Empty or whitespace-only queries return `{ "posts": [] }` without contacting PostgreSQL. Queries longer than 256 characters return `400`; an unconfigured database returns `503` and other storage failures return `500`. Search uses PostgreSQL's forgiving web search parser, which accepts ordinary multiword queries.

The CLI uses the same endpoint and accepts `--json`, `--jsonl`, `--api-url`, `KURA_API_URL`, `--limit`, and `--cursor`. Pagination flags are reported as unsupported until the API adds cursor pagination.

## Next milestone

The first search-to-MediaGrid slice is now wired through PostgreSQL and the CLI. The next product work can add the fuller lexer, parser, AST, validation, and query-planning layers, followed by real media fixtures and the frontend identity pass.
