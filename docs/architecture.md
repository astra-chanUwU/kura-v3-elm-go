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

This is a design boundary for the next milestone. The current scaffold keeps the server free of database dependencies and renders an Elm shell without making a network request.

## Server shape

`server/cmd/kura-server` is the executable. Reusable HTTP and domain packages belong under `server/internal/`, so the future CLI can call the public HTTP API instead of importing server implementation details. The intended database path is handwritten SQL, sqlc-generated types, and pgx; no ORM is planned.

Domain mutations create immutable revisions. A revert creates another revision that records what it reverses, preserving the complete history.

## Frontend shape

Elm code is organized by application, domain, API, page, feature, and UI boundaries. The first media slice should establish interfaces for `MediaGrid`, `Selection`, `QuickLook`, `QueryEditor`, and `TagEditor` before adding broad feature surface. Cursor-based result loading, keyboard navigation, density, and virtualization are later behaviors behind those interfaces.

## Deferred infrastructure

The foundation does not add Docker, Kubernetes, CI, Redis configuration, S3 configuration, authentication libraries, CSS or component frameworks, queues, realtime transports, or Rust crates. Add one only when a concrete Kura requirement and a bounded interface justify it.
