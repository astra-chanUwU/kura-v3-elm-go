# Daily library milestone

Release criterion: **I can safely manage my actual image library here every day.**

This is an implementation plan, not a claim that the features below are shipped.
Keep Elm, Go, PostgreSQL, and local media storage. Deliver browser write access
first, collection refresh second, and collection pagination third. Each slice
must pass automated checks and browser acceptance before moving to the next.

## Verified baseline — 2026-10-10

- `make check` now includes server tests/vet, CLI tests/build, the frontend build,
  and a runnable Elm harness. All 27 existing Elm assertions pass. The harness
  also fails on false results and missing reports.
- The server integration run passed 128 test cases, including subtests, with
  zero failures and zero skips against a newly created disposable PostgreSQL
  database. Test tables and media were separate from the user's library.
- Thirteen live HTTP workflow checks passed: health, search pagination, invalid
  queries, cursor binding, uploads, unchanged original bytes, thumbnail jobs,
  tag edits/conflicts/reverts, reactions, saved searches, collection mutations,
  and moderation hide/restore.
- Seven additional live protected-API checks passed: public health/search,
  owner-scoped saved-search access, missing/wrong-token rejection, and a valid
  token write. PATCH preflight is confirmed missing from CORS and remains an
  acceptance failure for the browser-write slice.
- Browser checks verified grid/Loupe, Inspector metadata and tag editing,
  Compare/Survey, two sequential uploads with ready thumbnails, and upload View
  returning to the original filtered sequence and selection.
- Browser removal reproduced a failing refresh check: Navigator changed from
  two members to one, while the grid and selection still showed both posts.
- A live 201-member collection returned only 60 posts with no next cursor;
  whole-array reorder of its 201 IDs returned `400`. These are confirmed
  acceptance failures, not passing workflow checks.
- Protected browser writes remain unimplemented. Real S3 deployment,
  large-library performance, and backup/restore are separate acceptance work.

Planning inputs: Muse Sparx 1.2 high reviewed browser writes; Muse Sparx 1.3 high
reviewed collections. Luna high implemented the check wiring. The supervising
agent reviewed code and corrected contract and concurrency assumptions.

## 1. Protected browser writes

### Scope and contract

Keep the existing single-owner `KURA_API_TOKEN` gate. Add an in-memory browser
unlock/lock flow; passwords, passkeys, persistent sessions, and multiple users
are separate work. This gate protects writes; it does not make public reads or
media private.

Add `GET /api/capabilities` to discover the mode and validate a candidate token
without creating or modifying a resource:

| Request | Response |
| --- | --- |
| Open local mode, no token | `200 {"writes_require_auth":false,"can_write":true,"actor":"local"}` |
| Gated mode, no token | `200 {"writes_require_auth":true,"can_write":false,"actor":null}` |
| Gated mode, valid bearer token | `200 {"writes_require_auth":true,"can_write":true,"actor":"system"}` |
| Gated mode, supplied invalid bearer token | `401 {"error":"write capability required"}` |

Return no token or token digest. Reuse the current token comparison and actor
resolution in `server/internal/httpapi/auth.go`; do not advertise an unvalidated
capability as granted. Fix the existing CORS method list to include `PATCH`.

### Browser behavior

- Model access explicitly: checking, open-local, locked, validating, unlocked,
  and unavailable. Unknown/unavailable mode disables writes with a retry action.
- Put Unlock/Lock in the top bar with a compact token field. Validate before
  declaring the browser unlocked. Wrong tokens leave it locked with a clear
  message. Open local mode continues to work without credentials.
- Keep token and draft only in transient Elm state. Clear the draft after
  validation and clear both on Lock/reload. Never persist them in preferences,
  localStorage, URLs, logs, or error text.
- Pass credentials explicitly to post/tag/reaction, upload, collection, and
  saved-search mutations. Authenticate saved-search reads because they are
  owner-scoped. Do not attach credentials to public post/collection/job reads,
  originals, thumbnails, or external media URLs.
- Disable write controls while gated and locked. Keep browsing and selection
  available. A `401` locks the UI and explains how to retry; never automatically
  resubmit a mutation or an upload whose result is uncertain.
- Keep local fallback searches separate from server searches. Unlock loads the
  `system` owner's server list; Lock clears that list and restores the local
  fallback. Do not silently merge or migrate `local`/`system` owners.
- Give credential-dependent requests an access generation. Lock/unlock
  invalidates old saved-search responses and prevents stale success/error UI.
  A request already accepted by the server may still finish after Lock: locking
  is not rollback. Reconcile public post/collection state and report uncertain
  upload outcomes; do not blindly discard durable changes or resend them.

### Implementation boundary and acceptance

Add a small `App.Access`/`Api.Access` boundary and extend the existing API
functions rather than rewriting `Page.Library`. Touch the relevant Go handler,
router/CORS tests, Elm API modules, Library state, and upload queue context.

Acceptance covers open local mode; public reads while locked; missing/wrong/
valid token; all mutation families; owner-scoped saved searches; PATCH preflight;
reload/Lock clearing credentials; stale responses; and uploads in progress
during Lock. Browser acceptance must verify the real write flow, not only mocks.

## 2. Collection edits refresh the displayed collection

`CollectionAdded`, `CollectionReordered`, and `CollectionPostRemoved` currently
update collection metadata without refreshing `sequence` in
`web/src/Page/Library.elm`.

- Carry the actual collection ID and operation/request generation in every
  mutation completion. Do not use whichever `activeCollection` happens to be
  selected when a delayed response arrives.
- Update metadata for the affected collection. Refresh the grid only when that
  collection matches `collectionBrowse`; changing a target collection must not
  replace an unrelated search or another collection's grid.
- Invalidate older browse/page requests on mutation. Use one explicit refresh
  path, distinct from opening a collection: keep stale results visible while
  loading, then replace them with the authoritative ordered results.
- Preserve selection and active post when still present, prune removed IDs,
  choose a nearby active survivor when necessary, and keep valid Loupe/Compare/
  Survey state. Preserve scroll and restore focus; avoid the current
  `openCollection` behavior that resets scroll to zero.
- With pagination, refresh the previously loaded prefix in bounded pages;
  do not shrink a multi-page workspace to page one or download the whole
  collection merely to reconcile an edit.
- Apply the existing ready-thumbnail overlay to collection results, as regular
  search already does.

Acceptance: add/remove/reorder while browsing that collection, edits to a
different target collection, navigation before responses arrive, mutation
failure, selection/active removal, and mode/scroll/focus preservation.

## 3. Collection pagination and ordering at useful sizes

The current browse endpoint returns at most 60 members with no next cursor.
The 200-ID limit is **per mutation request**, not a total collection-size cap.
Repeated additions can produce larger collections, while whole-array reorder
rejects an order containing more than 200 IDs. Do not introduce an artificial
200-member library limit.

### Cursor contract

- Extend `GET /api/collections/{id}/posts?limit=60&cursor=...`, retaining the
  existing `{posts,next_cursor}` envelope and adding `collection_version`.
- Use a dedicated versioned collection cursor, bounded and validated like the
  search cursor. Bind it to collection ID, membership/order version, and the
  `(position,post_id)` key. Fetch `limit + 1`; return null at the end.
- Add a collection membership/order version. Serialize add/remove/reorder on
  the collection row and increment it only for effective changes. Read the
  version and page from one consistent database snapshot.
- Reject malformed/cross-collection cursors with `400`. A valid cursor whose
  membership/order version has changed returns `409` with
  `{"error":"collection changed","code":"collection_changed"}`. Refresh the
  previously loaded window instead of silently skipping or duplicating rows.
- Keep current published/non-deleted visibility filtering and ready-thumbnail
  overlay. Versions describe membership/order, not a frozen moderation snapshot.

### Elm integration

Dispatch load-more by result source (query or collection) and request kind
(initial, append, refresh), with generation and collection-ID guards. Reuse the
current `nextCursor`/loading state and sequence append rules rather than adding
two competing pagination state machines. Collection responses must not clear
`collectionBrowse`; the current global-search append path does that.

Appending preserves selection, active post, mode, and scroll. Mutation success
and `collection_changed` invalidate outstanding append requests and use the
refresh path from slice 2. Query changes clear collection browsing as today.

Acceptance: 150 and 1,000 visible members; 60/60/30 pages; no gaps or duplicates
within unchanged membership/order; final cursor null; collection/cursor mismatch;
hidden members; ready thumbnails; mutation during page loading; stale response
rejection; and active/selection/scroll preservation.

### Follow immediately with bounded ordering

Pagination alone does not fix whole-array metadata, Navigator rendering, or
reordering beyond 200 members. Before calling large collections daily-use ready:

- Add a version-checked single-member move command (`post_id`, destination
  neighbor or end, `expected_version`) instead of sending every ID. Lock the
  collection and return the resulting version; stale moves return `409`.
- Return lightweight collection identity/count/version for navigation, and page
  the visible ordering UI. Preserve compatibility for existing CLI/API clients
  through additive fields or an explicitly separate summary endpoint.
- Test moving first/last members, cross-page moves, concurrent changes, missing
  neighbors, and hidden membership without silently losing order.

## Following milestone

After these slices pass: prove database-plus-media backup/restore, add resumable
folder import with explicit duplicate handling, and measure a representative
real library. These are required evidence for the release criterion; passing
small fixture tests alone does not establish it.
