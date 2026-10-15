package main

const rootHelp = `kura - Kura media browser CLI (HTTP API only)

Usage:
  kura <command> [flags] [args]
  kura --help
  kura search --help
  kura collection --help
  kura post --help
  kura tag --help
  kura login --help
  kura logout --help

Commands:
  search    Search posts via GET /api/posts
  collection List collections or browse collection posts
  post      Show post detail and mutate tags, favorites, scores, and content ratings
  export    Export posts as a ZIP archive file
  tag       List and manage global tags
  login     Sign in as a named account (password from the terminal or stdin)
  logout    Revoke the logged-in account session

Environment:
  KURA_API_URL    Base API URL (default http://localhost:8080)
  KURA_API_TOKEN  Deployment admin bearer token (optional)
  KURA_PRIVATE_READS  Opt into bearer auth on library GET reads for private deployments (default false; accepts true/false/1/0; requires KURA_API_TOKEN)
  KURA_HTTP_TIMEOUT  Per-request duration (default 10s; e.g. 30s)
  KURA_SESSION_FILE  Optional path for kura login --persist (default: user config directory)
  KURA_AUTH_TRUSTED_ORIGIN  Exact Origin for account-session writes, when the server requires one

Authentication precedence:
  1. --api-token VALUE  deployment admin token; overrides KURA_API_TOKEN
  2. KURA_API_TOKEN     deployment admin token
  3. kura login         role-limited account session, in memory or --persist
  A deployment token is never combined with the logged-in account. Prefer
  KURA_API_TOKEN over --api-token so the token is not placed on the command line.
  Passwords are read from the terminal or stdin only.

Notes:
  The CLI uses only the public HTTP API (same as the Elm app). No database
  access from the CLI; a running Kura API is required. It defaults to
  http://localhost:8080. Writes are never automatically retried.
  Library GET reads stay unauthenticated by default; set KURA_PRIVATE_READS=true
  (with KURA_API_TOKEN) only against a private deployment so search,
  collection, post detail, and ZIP exports send the configured API base the
  bearer. A logged-in account session is sent on reads and writes without
  that flag, and only when no deployment token is set. Redirects are never
  followed and preview/original image URLs are never fetched.
  Run with --help on any command for details.
`

const tagHelp = `kura tag - list and manage global tags

Usage:
  kura tag list [flags]
  kura tag rename [flags] SOURCE TARGET
  kura tag merge [flags] SOURCE TARGET

Commands:
  list    Read one bounded page of global tags
  rename  Preview by default; apply with an explicitly observed --version
  merge   Preview by default; apply with an explicitly observed --version

Run kura tag list --help, kura tag rename --help, or kura tag merge --help for details.
`

const tagListHelp = `kura tag list - list global tags

Usage:
  kura tag list [flags]

Reads one page from the protected GET /api/tags/manage endpoint.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL)
  --prefix TEXT     Filter tags by prefix
  --limit N         Page size 1..60 (default 60)
  --cursor CURSOR   Opaque cursor from the previous page
  --json            Output the full response envelope
  --jsonl           Output one tag object per line
  -h, --help        Show this help

The response includes taxonomy version and next_cursor. Each invocation reads
exactly one page; pass its cursor to continue. A bearer token from
KURA_API_TOKEN is sent for this protected endpoint.
`

func tagMutationHelp(mode string) string {
	return `kura tag ` + mode + ` - ` + mode + ` global tags

Usage:
  kura tag ` + mode + ` [flags] SOURCE TARGET

By default this sends a non-mutating preview request. To apply the operation,
pass --version N using the taxonomy version observed from the preview. Version
zero is valid. Writes are never retried automatically.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL)
  --preview         Explicitly request a preview (default behavior)
  --version N       Apply using the expected taxonomy version (non-negative int64)
  --json            Output the response as pretty JSON
  --jsonl           Output the response as one JSON Lines object
  -h, --help        Show this help

--preview and --version cannot be combined. SOURCE and TARGET are canonical
tag names.
`
}

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
  kura collection summaries [flags]
  kura collection posts [flags] COLLECTION_ID
  kura collection create [flags] NAME
  kura collection add [flags] COLLECTION_ID POST_ID...
  kura collection remove [flags] COLLECTION_ID POST_ID
  kura collection reorder [flags] COLLECTION_ID [POST_ID...]
  kura collection move [flags] COLLECTION_ID POST_ID
  kura collection rename [flags] COLLECTION_ID NAME
  kura collection delete [flags] COLLECTION_ID
  kura collection export [flags] COLLECTION_ID

Commands:
  list     List collections via GET /api/collections
  summaries List compact counts via GET /api/collections/summaries
  posts    List posts in a collection via GET /api/collections/{id}/posts
  create   Create a collection via POST /api/collections
  add      Add posts via POST /api/collections/{id}/posts
  remove   Remove one post via DELETE /api/collections/{id}/posts/{postID}
  reorder  Replace collection order via POST /api/collections/{id}/order
  move     Move one post via POST /api/collections/{id}/move
  rename   Rename a collection via PATCH /api/collections/{id}
  delete   Delete a collection via DELETE /api/collections/{id}
  export   Export visible members as a ZIP via GET /api/collections/{id}/export

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per collection, post, or mutation
                    (JSON Lines)
  --expected-version N  Required observed version for move, rename, and delete
  --before POST_ID   Move before a post (use exactly one of --before or --end)
  --end              Move to the end (use exactly one of --before or --end)
  --yes              Confirm collection delete
  -h, --help        Show this help

Examples:
  kura collection list
  kura collection list --json
  kura collection summaries
  kura collection posts 7
  kura collection posts --jsonl 7
  kura collection create "Reference set"
  kura collection add 7 11 10
  kura collection remove 7 11
  kura collection reorder 7 10 11
  kura collection move --expected-version 4 --before 11 7 10
  kura collection rename --expected-version 4 7 "Reference set"
  kura collection delete --expected-version 4 --yes 7
`

const collectionSummariesHelp = `kura collection summaries - list compact collection counts

Usage:
  kura collection summaries [flags]

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON
  --jsonl           Output one summary per line
  -h, --help        Show this help
`

const collectionMoveHelp = `kura collection move - move one post within a collection

Usage:
  kura collection move --expected-version N (--before POST_ID | --end) COLLECTION_ID POST_ID

Flags:
  --expected-version N  Required observed collection version (zero is valid)
  --before POST_ID      Move before this member
  --end                 Move to the end
  --api-url URL         Base API URL
  --json | --jsonl      Output compact result
  -h, --help            Show this help
`

const collectionRenameHelp = `kura collection rename - rename a collection

Usage:
  kura collection rename --expected-version N [flags] COLLECTION_ID NAME

Flags:
  --expected-version N  Required observed collection version (zero is valid)
  --api-url URL         Base API URL
  --json | --jsonl      Output compact result
  -h, --help            Show this help
`

const collectionDeleteHelp = `kura collection delete - delete a collection and its memberships

Usage:
  kura collection delete --expected-version N --yes [flags] COLLECTION_ID

Flags:
  --expected-version N  Required observed collection version (zero is valid)
  --yes                 Confirm deletion
  --api-url URL         Base API URL
  --json | --jsonl      Output compact result
  -h, --help            Show this help
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
every current visible member exactly once. Use collection move for a compact,
version-checked request instead of sending the entire order.

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
  metadata  Edit source/artist via POST /api/posts/metadata
  trash     Move posts to trash via POST /api/posts/trash
  restore   Restore trashed posts via POST /api/posts/restore
  trash-list List trashed posts via GET /api/posts/trash
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
  kura post metadata --version 0 --source "scan" 2004
  kura post trash --reason duplicate 2004
  kura post restore 2004
  kura post trash-list --json
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

const postMetadataHelp = `kura post metadata - edit source/artist via the HTTP API

Usage:
  kura post metadata --version N [--source VALUE] [--artist VALUE] POST_ID

At least one of --source or --artist is required. An omitted field stays
unchanged; an explicitly empty value clears it. Values are trimmed and must
be at most 512 characters. Version zero is valid.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Required observed metadata_version
  --source VALUE    New source value
  --artist VALUE    New artist value
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postTrashHelp = `kura post trash - move posts to trash via the HTTP API

Usage:
  kura post trash [--reason VALUE] POST_ID...

Accepts 1 to 100 distinct post ids. Trashing is reversible with restore.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --reason VALUE    Optional reason (at most 512 characters)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postRestoreHelp = `kura post restore - restore trashed posts via the HTTP API

Usage:
  kura post restore POST_ID...

Accepts 1 to 100 distinct post ids.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the response as JSON (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postTrashListHelp = `kura post trash-list - list trashed posts via the HTTP API

Usage:
  kura post trash-list [flags]

Reads one bounded page from the protected GET /api/posts/trash endpoint. A
Bearer [REDACTED] from KURA_API_TOKEN is sent for this protected endpoint.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --limit N         Page size 1..60
  --cursor CURSOR   Opaque cursor from the previous page
  --json            Output the full response envelope
  --jsonl           Output one trashed post object per line
  -h, --help        Show this help
`

const exportHelp = `kura export - export posts as a ZIP archive via the HTTP API

Usage:
  kura export --ids ID,ID,... --output PATH [--metadata]

Downloads GET /api/posts/export to PATH. The destination must not already
exist; the archive is streamed to a temporary file and published atomically.
ZIP bytes are never written to stdout.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --ids IDS         Comma-separated distinct post ids (1 to 1000, required)
  --output PATH     Destination ZIP path (required, must not exist)
  --metadata        Include metadata in the archive
  --json            Output a {path,bytes} receipt as JSON
  --jsonl           Output a {path,bytes} receipt as one JSON Lines object
  -h, --help        Show this help
`

const collectionExportHelp = `kura collection export - export a collection as a ZIP archive

Usage:
  kura collection export --version N --output PATH [--metadata] COLLECTION_ID

Downloads GET /api/collections/{id}/export with the observed collection
version. Only visible members are exported. The destination must not already
exist; ZIP bytes are never written to stdout.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Required observed collection version (zero is valid)
  --output PATH     Destination ZIP path (required, must not exist)
  --metadata        Include metadata in the archive
  --json            Output a {path,bytes} receipt as JSON
  --jsonl           Output a {path,bytes} receipt as one JSON Lines object
  -h, --help        Show this help
`

const postReactionHelp = `kura post reaction - set favorite and/or score via the HTTP API

Usage:
  kura post rating --version N [flags] POST_ID RATING
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
