# Kura V3 architecture

Kura uses a browser application and one Go server around durable PostgreSQL records. The browser owns interaction state; the server owns authentication, authorization, domain mutations, and query execution. PostgreSQL remains the source of durable truth, while object storage owns large media derivatives and originals behind an abstraction. Redis may be added later for shared ephemeral state and must never be required to recover durable records.

## Request flow

The first serious vertical slice has this boundary:

```text
Elm search page
    ↓
GET /api/posts?q=...
    ↓
Go HTTP handler and query service
    ↓
PostgreSQL
    ↓
post summaries
    ↓
Elm MediaGrid
```

The Go adapter now executes this boundary against PostgreSQL. `db/migrations/001_posts.sql` defines the small searchable `posts` table, and `db/seed.sql` provides deterministic local rows for development. Twelve matching demo files live under `web/static/media/demo`; the server exposes them at `/media/...` from `MEDIA_ROOT` (defaulting to `../web/static/media` for `go -C server run`).

The API response keeps the browser contract independent from PostgreSQL
rows. Search uses an opaque, query-bound keyset cursor (`id DESC`) and accepts
`limit=1..60` (default 60); the adapter fetches one extra row to decide whether
to emit `next_cursor`.

```http
GET /api/posts?q=cat
```

```json
{
  "posts": [
    {
      "id": "post-123",
      "preview_url": "/media/post-123/preview.jpg",
      "original_url": "/media/post-123/original.jpg",
      "media_type": "image/jpeg",
      "width": 640,
      "height": 480
    }
  ]
}
```

The HTTP layer depends on a `posts.Searcher` interface. The PostgreSQL adapter
binds the search string to the handwritten query and maps selected columns to
`PostSummary`; it does not expose database rows directly. Empty queries browse
newest visible rows through the same keyset cursor, while an unconfigured
adapter returns `503` and storage failures return `500`. Overlong queries,
invalid limits, and malformed or query-mismatched cursors return `400`.
PostgreSQL's web search parser accepts ordinary multiword input.

The Inspector loads read-only metadata on demand from `GET /api/posts/{id}`.
The detail response extends `PostSummary` with source, artist, hash, file size,
creation time, and tags; deleted rows return `404` and an unconfigured detail
adapter returns `503`.

Tag edits use `POST /api/posts/tags` with selected post ids, their expected
tag versions, and add/remove lists. The server locks all targets in one
transaction, rejects stale versions with `409`, updates the generated search
text, increments each changed post's version, and records an immutable tag
revision. The detail response includes the current version and the latest 50
tag revisions.

## Server shape

`server/cmd/kura-server` is the executable. Reusable HTTP and domain packages belong under `server/internal/`, so the future CLI can call the public HTTP API instead of importing server implementation details. The intended database path is handwritten SQL, sqlc-generated types, and pgx; no ORM is planned.

Domain mutations create immutable revisions. A revert creates another revision that records what it reverses, preserving the complete history.

## Frontend shape

Elm code is organized by application, domain, API, page, feature, and UI boundaries. `Page.Library` owns the workspace state (result sequence, active post and selection, mode, scroll return point, panels); `Domain.Sequence` and `Domain.Selection` hold the pure rules; `Feature.*` modules render the MediaGrid, Quick Look, Compare, Survey, Filmstrip, Inspector, Navigator, and query editor; `Ui.*` holds small shared view helpers. Styles live in plain CSS at `web/kura.css`, and workspace preferences persist in `localStorage` through the `savePrefs` port. The grid is virtualized; cursor-based result loading appends deduplicated pages while preserving active selection, mode, and scroll; tag edits, favorites, and scores use optimistic versions and immutable revisions. Collections are ordered memberships with selection-driven add and compact Navigator reordering. See `docs/frontend-design.md`.

## Deferred infrastructure

The foundation does not add Docker, Kubernetes, CI, Redis configuration, S3 configuration, authentication libraries, CSS or component frameworks, queues, realtime transports, or Rust crates. Add one only when a concrete Kura requirement and a bounded interface justify it. PostgreSQL setup is intentionally limited to `make db-migrate` and the explicit `make db-seed` development command, which require a local `psql` client and `DATABASE_URL`; no deployment workflow is implied.
