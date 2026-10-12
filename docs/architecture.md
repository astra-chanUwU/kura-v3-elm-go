# Kura architecture

Kura is a modular monolith with explicit HTTP and domain boundaries. Elm owns
browser interaction state; the Go API owns authorization and library operations;
PostgreSQL owns durable records. Media bytes live behind a storage interface,
and a worker in the API process executes durable derivative jobs.

## Stack and reasons

- **Elm** gives the browser a consistent Model / Update / View structure.
  Selection, navigation, editing, and upload phases are explicit state rather
  than behavior scattered across event handlers. Pure rules can be tested
  separately from rendering and HTTP.
- **Go + Chi** provides explicit routes and middleware while retaining ordinary
  `net/http` handlers. Chi is the HTTP router, rather than the architecture of
  the entire application.
- **PostgreSQL + pgx** provides transactions, optimistic revision checks,
  ordered memberships, and durable job leases. SQL is currently handwritten;
  sqlc generation is not part of the current build.
- **HTTP and JSON** connect Elm and the CLI to the same application boundary.
  The API returns display-oriented contracts such as `PostSummary` and
  `PostDetail`, rather than exposing database rows directly. Elm decoders check
  responses at runtime; the two languages do not share a compiler or types.

Elm's state model is useful for this interactive library, with the cost of
learning Elm and using ports when browser integration requires JavaScript.
These choices serve Kura's current needs; they are not a requirement for every
application of this kind.

Official references: [Elm Architecture](https://guide.elm-lang.org/architecture/),
[Elm interoperability](https://guide.elm-lang.org/interop/),
[Chi](https://github.com/go-chi/chi), [Go](https://go.dev/doc/),
[pgx](https://github.com/jackc/pgx),
[PostgreSQL](https://www.postgresql.org/docs/).

## Where to start

```text
server/cmd/kura-server/main.go     Construct dependencies and manage lifecycle
server/internal/httpapi/router.go Chi routes and middleware, grouped by feature
server/internal/httpapi/          HTTP handlers, parsing, and response mapping
server/internal/posts/            Library contracts, rules, and pgx operations
server/internal/auth/             Actor identity and capability rules
server/internal/media/            Storage providers and derivative processing
server/internal/jobs/             Durable job store, leases, retries, and worker

db/migrations/                    PostgreSQL schema changes
web/src/Main.elm                  Browser application entry point
web/src/Page/Library.elm          Workspace state and feature coordination
web/src/Api/                      HTTP requests
web/src/Domain/                   Pure library types and rules
web/src/Feature/                  Feature state, updates, and views
web/src/App/                      Routes, access, preferences, and keyboard logic
web/src/Ui/                       Shared view helpers
cli/internal/client/              HTTP client used by the CLI
```

`main.go` is the server's composition root: it creates one PostgreSQL-backed
library adapter, shares its pool with the job store and derivative processor,
starts the worker, and passes the adapter into the HTTP router. Construction
uses ordinary Go constructors. Shutdown and a fatal worker exit are handled
there too.

Development uses two servers: the API on `:8080` and the static frontend server
on `127.0.0.1:8000` (`server/cmd/kura-web`). The frontend is not currently
embedded in the API binary. See the README for startup and configuration.

## Request flow

```text
Elm Api.* or CLI client
    → HTTP request
    → Chi router and middleware
    → httpapi feature handler
    → posts contract and operation
    → PostgreSQL / media storage / durable jobs
    → API response
    → Elm message → update → view, or CLI output
```

For example, `Api.Post.search` calls `GET /api/posts`. `router.go` maps that
endpoint to `searchPosts` in `httpapi/posts.go`. The handler parses the query,
limit, and cursor, then calls `posts.Searcher.SearchPosts`. The PostgreSQL
adapter executes a parameterized query and returns post summaries. Elm decodes
those summaries into the library's result sequence.

`GET /api/posts/{id}` loads detail for the Inspector. Tag and reaction writes
use expected versions; stale updates return `409`. A tag revert creates a new
revision rather than rewriting history. Collections use the same library
contracts and keep ordered membership in PostgreSQL.

## Server boundaries

The HTTP package owns request decoding, transport-level validation, status
codes, and response encoding. `router.go` contains route registration and
middleware assembly only; feature files contain the handlers:

- `posts.go`: search and detail.
- `tags.go`, `reactions.go`: edits and tag reverts.
- `collections.go`, `saved_searches.go`: library organization.
- `uploads.go`, `jobs.go`: ingestion and processing status.
- `moderation.go`, `media.go`: moderation and local media visibility.
- `auth.go`, `capabilities.go`, `cors.go`: HTTP access policy and metadata.
- `json.go`: shared JSON decoding and response helpers.

Domain contracts and library rules live in `posts`, with PostgreSQL operations
in its `postgres*`, `*_postgres`, and related SQL files. This package currently
contains both business behavior and persistence. Kura does not introduce a
service, repository, and DAO wrapper for every operation. A new layer should
have a distinct responsibility before it is added.

Keep these boundaries when changing the server:

- Register endpoints centrally so the route and middleware map stays readable.
- HTTP handlers call domain contracts; they do not execute SQL or own database
  pools. Domain packages do not depend on Chi or HTTP request contexts.
- Keep library validation and durable invariants in the domain operations;
  browser checks are conveniences, not authorization.
- Keep storage providers behind `media.Store` and job lifecycle rules in `jobs`.
- Keep dependency construction in the executable. The CLI uses the HTTP API
  instead of importing server internals.

Production constructs `posts.PostgresBackend` and supplies each narrow
capability through `httpapi.Dependencies` to `NewRouterWithDependencies`.
Handlers receive their required interfaces directly. The older Searcher-based
constructors retain capability discovery in a compatibility adapter only.
Absent capabilities retain their `503` responses.

## Middleware and access

`router.go` makes the order explicit: local-development CORS runs first, then
`requireWriteCapability`, then route dispatch. CORS handles preflight without
requiring credentials. The capability middleware resolves the actor for both
reads and writes and requires the configured bearer token for API mutations,
including unknown API write paths. Keeping it at the root preserves that policy.

An empty `KURA_API_TOKEN` enables local mode. A configured token represents the
system capability; it is not a multi-user login system. Public reads, health,
and preflight remain available. Capability discovery reports whether the
current request may write, and moderation handlers make their additional actor
checks through `auth`.

Local `/media/*` requests also check post visibility: referenced media for
hidden, rejected, or deleted posts is unavailable to public browsing. Remote
object URLs need access controls at the storage provider; the local HTTP gate
does not protect them.

## Upload and worker flow

An upload passes bounded multipart input to the library operation. Originals
are written through `media.Store`; post and derivative-job records are saved
in PostgreSQL. Local storage is content-addressed and uses an atomic file
handoff. Failed database writes clean up bytes created by that request without
deleting a pre-existing object.

The worker claims durable jobs, maintains leases, processes derivatives, and
records completion or failure. Startup recovery reclaims expired work. The API
and worker share one pool; the browser polls job status through the HTTP API.

## Frontend boundaries

`Main` installs `Page.Library` into `Browser.application`. The page coordinates
workspace state: search results, selection, active post, mode, panels, and scroll
return. Search, Tags, Reactions, and Collections own their request lifecycles
and report page-owned effects through output messages. The upload queue
separates pure transitions in `Feature.UploadQueue.Queue`, browser and HTTP
effects in its coordinator, and rendering in `Feature.UploadQueue.View`.
`Domain.*` holds pure types and rules, including shared upload limits and job
status facts. `Api.*` handles requests, response decoding, and shared HTTP-error
classification; feature-specific error wording stays with each feature.
`Ui.*` supplies small shared view helpers. CSS lives in `web/kura.css`.

Keep pure domain rules independent of rendering and HTTP. Coordinate features
through the page's messages and updates, and put new endpoint calls in `Api.*`.
Browser integrations such as preference persistence use ports with the hosting
page. See [frontend design](frontend-design.md) for the interaction model.

## Adding behavior and checking it

Start with the domain contract and durable invariant, implement the operation,
then add the HTTP handler in the matching feature file and register its route
in `router.go`. Add Elm API decoding and feature/page updates when the browser
needs the behavior; the CLI can consume the same endpoint.

Run `make server-check` for server tests and vet. HTTP tests use `httptest` and
fake domain implementations; they check routing, parsing, access, and error
mapping without a database. PostgreSQL integration tests require a disposable
`KURA_TEST_DATABASE_URL` and otherwise skip. Use `make web-test` for Elm logic
changes and `make check` for changes spanning the server, browser, and CLI.
Server and frontend tests enforce import boundaries. The frontend checker also
protects the pure upload queue and separates views from API effects.
Automated checks do not establish browser or storage-provider interoperability.
