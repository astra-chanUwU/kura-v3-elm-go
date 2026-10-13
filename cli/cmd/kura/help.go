package main

const rootHelp = `kura - Kura media browser CLI (HTTP API only)

Usage:
  kura <command> [flags] [args]
  kura --help
  kura search --help
  kura collection --help
  kura post --help

Commands:
  search    Search posts via GET /api/posts
  collection List collections or browse collection posts
  post      Show post detail and mutate tags, favorites, and scores

Environment:
  KURA_API_URL    Base API URL (default http://localhost:8080)
  KURA_API_TOKEN  Bearer token for write capabilities (optional)
  KURA_HTTP_TIMEOUT  Per-request duration (default 10s; e.g. 30s)

Notes:
  The CLI uses only the public HTTP API (same as the Elm app). No database
  access from the CLI; a running Kura API is required. It defaults to
  http://localhost:8080. Writes are never automatically retried.
  Run with --help on any command for details.
`

const searchHelp = `kura search - search posts via the HTTP API

Usage:
  kura search [flags] [QUERY]

Search posts using GET /api/posts?q=QUERY. QUERY is optional; when omitted
or empty the server browses newest visible posts. Multi-word queries may be
quoted or passed as separate words:

  kura search cat
  kura search "cat feline"
  kura search --json cat feline

Parser:
  Flags must appear before QUERY. Flags after QUERY are treated as part of
  QUERY. Use -- to end flag parsing explicitly:

  kura search --json -- cat --json   # queries for "cat --json"

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output response JSON (pretty-printed)
  --jsonl           Output one JSON object per post (JSON Lines)
  --limit N         Page size 1..60 (max 60, omit for server default)
  --cursor CURSOR   Opaque pagination cursor from previous next_cursor
  --all             Follow all pages (guarded against loops; at most 1,000 pages)
  -h, --help        Show this help

Output:
  Default (human)   Tab-separated lines: ID  MEDIA_TYPE  WxH  PREVIEW_URL  ORIGINAL_URL
                    Empty results produce no stdout (exit 0). Errors go to stderr.
  --json            Pretty-printed JSON envelope from the API (includes posts and
                    optional next_cursor).
  --jsonl           One JSON object per post, one line each. Empty results produce
                    no stdout. next_cursor, if present, is written to stderr as a hint.

Pagination:
  GET /api/posts?q=&cursor=&limit= returns {posts:[...],next_cursor:string|null}.
  Use --limit 1..60 and --cursor from the previous response to page. Each
  invocation makes one request unless --all is supplied. --all stops on errors
  without restarting or retrying. JSON Lines may contain earlier completed pages
  if a later page fails. --all --json is rejected because it would buffer every
  page in memory; use --all --jsonl to stream output instead.

Examples:
  kura search --api-url http://localhost:8080 cat
  KURA_API_URL=http://localhost:8080 kura search --json anime
  kura search --jsonl "landscape mountain"
  kura search --limit 20 --cursor tok123 cat
`

const collectionHelp = `kura collection - list, browse, and mutate collections

Usage:
  kura collection list [flags]
  kura collection posts [flags] COLLECTION_ID
  kura collection create [flags] NAME
  kura collection add [flags] COLLECTION_ID POST_ID...
  kura collection remove [flags] COLLECTION_ID POST_ID
  kura collection reorder [flags] COLLECTION_ID [POST_ID...]

Commands:
  list     List collections via GET /api/collections
  posts    List posts in a collection via GET /api/collections/{id}/posts
  create   Create a collection via POST /api/collections
  add      Add posts via POST /api/collections/{id}/posts
  remove   Remove one post via DELETE /api/collections/{id}/posts/{postID}
  reorder  Replace collection order via POST /api/collections/{id}/order

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per collection, post, or mutation
                    (JSON Lines)
  -h, --help        Show this help

Examples:
  kura collection list
  kura collection list --json
  kura collection posts 7
  kura collection posts --jsonl 7
  kura collection create "Reference set"
  kura collection add 7 11 10
  kura collection remove 7 11
  kura collection reorder 7 10 11
`

const collectionListHelp = `kura collection list - list collections via the HTTP API

Usage:
  kura collection list [flags]

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per collection (JSON Lines)
  -h, --help        Show this help
`

const collectionPostsHelp = `kura collection posts - list posts in a collection via the HTTP API

Usage:
  kura collection posts [flags] COLLECTION_ID

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per post (JSON Lines)
  --limit N         Page size 1..60 (omit for server default)
  --cursor CURSOR   Opaque cursor from the previous page
  --all             Follow all pages (at most 1,000); stop if the collection changes
  -h, --help        Show this help
`

const collectionCreateHelp = `kura collection create - create a collection via the HTTP API

Usage:
  kura collection create [flags] NAME

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the created collection as JSON
  --jsonl           Output the created collection as one JSON Lines object
  -h, --help        Show this help
`

const collectionAddHelp = `kura collection add - add posts to a collection via the HTTP API

Usage:
  kura collection add [flags] COLLECTION_ID POST_ID...

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the updated collection as JSON
  --jsonl           Output the updated collection as one JSON Lines object
  -h, --help        Show this help
`

const collectionRemoveHelp = `kura collection remove - remove a post via the HTTP API

Usage:
  kura collection remove [flags] COLLECTION_ID POST_ID

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output a removal result as JSON
  --jsonl           Output a removal result as one JSON Lines object
  -h, --help        Show this help
`

const collectionReorderHelp = `kura collection reorder - replace collection order via the HTTP API

Usage:
  kura collection reorder [flags] COLLECTION_ID [POST_ID...]

Pass no POST_ID values to clear a collection. A non-empty order must contain
every current member exactly once. The API accepts at most 200 IDs per request.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the updated collection as JSON
  --jsonl           Output the updated collection as one JSON Lines object
  -h, --help        Show this help
`

const postHelp = `kura post - post detail and mutations via the HTTP API

Usage:
  kura post <command> [flags] [args]
  kura post --help
  kura post show --help
  kura post tag --help
  kura post favorite --help
  kura post score --help
  kura post reaction --help

Commands:
  show      Show post detail with revision history via GET /api/posts/{id}
  get       Alias for show
  tag       Edit or revert tags (subcommands: edit, revert)
  favorite  Set favorite via POST /api/posts/reactions
  score     Set score via POST /api/posts/reactions
  reaction  Set favorite and/or score via POST /api/posts/reactions

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output JSON Lines
  -h, --help        Show this help

Examples:
  kura post show 2004
  kura post show --json 2004
  kura post tag edit --version 2 --add night 2004
  kura post tag revert --version 3 --target-version 1 2004
  kura post favorite --version 1 --favorite=true 2004
  kura post score --version 1 --score 5 2004
  kura post reaction --version 1 --favorite=true --score 5 2004
`

const postShowHelp = `kura post show - show post detail via the HTTP API

Usage:
  kura post show [flags] POST_ID

Shows post detail including tags, source, artist, hash, file size, dimensions,
favorite, score, versions, and revision history from GET /api/posts/{id}.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object (JSON Lines, compact)
  -h, --help        Show this help
`

const postTagHelp = `kura post tag - edit or revert tags via the HTTP API

Usage:
  kura post tag edit [flags] POST_ID
  kura post tag revert [flags] POST_ID

Commands:
  edit    Edit tags via POST /api/posts/tags
  revert  Revert tags via POST /api/posts/tags/revert

Flags for edit:
  --api-url URL       Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N         Expected current tag_version (required)
  --add TAG           Tag to add (repeatable, or comma-separated)
  --remove TAG        Tag to remove (repeatable, or comma-separated)
  --json              Output the response as JSON (pretty-printed)
  --jsonl             Output one JSON object per result (JSON Lines)
  -h, --help          Show this help

Flags for revert:
  --api-url URL       Base API URL
  --version N         Expected current tag_version (required)
  --target-version N  Historical version to restore (required)
  --json              Output the response as JSON
  --jsonl             Output JSON Lines
  -h, --help          Show this help

Examples:
  kura post tag edit --version 0 --add night --remove demo 2004
  kura post tag revert --version 2 --target-version 1 2004
`

const postTagEditHelp = `kura post tag edit - edit tags via the HTTP API

Usage:
  kura post tag edit [flags] POST_ID

Edits tags via POST /api/posts/tags with optimistic version checking.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current tag_version (required)
  --add TAG         Tag to add (repeatable)
  --remove TAG      Tag to remove (repeatable)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postTagRevertHelp = `kura post tag revert - revert tags via the HTTP API

Usage:
  kura post tag revert [flags] POST_ID

Reverts tags to a historical revision via POST /api/posts/tags/revert with
optimistic version checking.

Flags:
  --api-url URL       Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N         Expected current tag_version (required)
  --target-version N  Historical version to restore (required)
  --json              Output the response as JSON (pretty-printed)
  --jsonl             Output one JSON object per result (JSON Lines)
  -h, --help          Show this help
`

const postFavoriteHelp = `kura post favorite - set favorite via the HTTP API

Usage:
  kura post favorite [flags] POST_ID

Sets favorite via POST /api/posts/reactions with optimistic version checking.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current reaction_version (required)
  --favorite BOOL   Favorite value true or false (required)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postScoreHelp = `kura post score - set score via the HTTP API

Usage:
  kura post score [flags] POST_ID

Sets score via POST /api/posts/reactions with optimistic version checking.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current reaction_version (required)
  --score N         Score value 0..2147483647 (required)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postReactionHelp = `kura post reaction - set favorite and/or score via the HTTP API

Usage:
  kura post reaction [flags] POST_ID

Sets favorite and/or score via POST /api/posts/reactions with optimistic version
checking. At least one of --favorite or --score is required.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current reaction_version (required)
  --favorite BOOL   Favorite value true or false
  --score N         Score value 0..2147483647
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`
