# Kura V3

Kura is an imageboard and media-management application. This repository is the small foundation for the V3 Elm and Go implementation.

## Initial boundaries

- **Elm** owns browser state, routing, search and media-management interaction.
- **Go** owns the HTTP API and domain behavior. The intended routing and database stack is chi, pgx, and sqlc over PostgreSQL; database work starts after this scaffold.
- **PostgreSQL** will be durable structured truth. Large media will live behind an S3-compatible object-store abstraction.
- **Redis, realtime transports, Rust services, queues, and deployment infrastructure** are deliberately deferred until a concrete product requirement needs them.
- **The `kura` CLI** will use the same HTTP API as Elm. Its directory is reserved while the domain API is designed.

The first product slice is the media browsing foundation: MediaGrid, selection, quick look, query editing, and tag editing. The current code only establishes the boundaries and a working shell.

## Repository layout

```text
web/       Elm application
server/    Go HTTP server and future domain packages
cli/       CLI entry point reserved for the shared HTTP API client
db/        PostgreSQL migrations and handwritten SQL queries
scripts/   small repository scripts
docs/      architecture and domain notes
```

## Run the scaffold

From the repository root:

```sh
make server-run       # listens on HTTP_ADDR (default :8080)
make web-build        # writes web/dist/elm.js
make web-serve        # serves web/ on http://localhost:8000
```

The health endpoint is `GET http://localhost:8080/health` and returns a small JSON status response.

## Next milestone

Implement the first vertical slice at the documented boundary: `Elm search page → GET /api/posts?q=... → Go query service → PostgreSQL → post summaries → Elm MediaGrid`. The initial query language should be represented as lexer, parser, AST, validation, and query-planning layers rather than splitting a search string on spaces.
