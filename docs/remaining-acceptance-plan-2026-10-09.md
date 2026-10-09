# Remaining acceptance work — 2026-10-09

Baseline: `abba18c`, with the five Muse implementation slices and the
[acceptance audit](acceptance-audit-2026-10-09.md) committed. Server tests,
Go vet, CLI tests/build, Elm build, and `git diff --check` were rerun and
passed. The earlier CLI sandbox blocker did not recur.

Deliver these two slices in order. Use a fresh Muse Spark 1.3 process at
high reasoning for each slice, followed by checkout and result review.
This is a plan; runtime integration and live mutation acceptance remain open.

## 1. Connect uploads, durable jobs, and thumbnails

Outcome: a successful upload durably queues a thumbnail, the in-process
worker produces it, and API readers can discover its job and variant status.

### Implementation

1. Compose the posts service, job store, variant recorder, and media provider
   explicitly at server startup. Prefer one PostgreSQL pool with clear
   ownership. Inject a job reader into the HTTP router rather than requiring
   the posts searcher to implement an unrelated interface. Preserve current
   router constructors for existing callers/tests.
2. Start one worker restricted to `media.KindDerivative`, using the same
   configured media provider for originals and thumbnails. Cancel and wait
   for the worker during graceful server shutdown before closing its pool.
   Surface worker exit errors rather than silently leaving uploads unprocessed.
3. Insert the post and its default `thumb-320` job in the same upload
   transaction. Extend the job enqueue boundary to accept that transaction;
   preserve its existing idempotency key. A failed transaction must leave
   neither a post nor a job, and clean up only newly created original objects.
4. Expose additive job/variant metadata through upload or post-detail responses
   so clients can discover the polling ID. Keep existing response fields
   compatible. Use the original as preview until a ready variant exists,
   then return the thumbnail as `preview_url`; preserve `original_url`.
5. Include variant URLs in moderation visibility lookup, even before a
   thumbnail becomes the selected preview. Hidden, rejected, or deleted
   posts must not expose their thumbnails through direct local media URLs.
6. Complete restart recovery: the current claim query selects pending jobs
   only, so expired running jobs need bounded reclamation. Enforce lease
   ownership/expiry when completing work and cancel processing after lease
   loss. Verify retry limits and prevent stale workers from completing a
   reclaimed job.

Use the current `media.Store` contract (`Put`, `Open`, `Delete`); the
derivative slice already added the read method. Start with local media and
the existing JPEG/PNG/GIF support. Preserve S3 compatibility through provider
contract checks; live S3 moderation/access behavior needs separate evidence
before claiming it works. Keep this slice in Go and narrowly needed SQL.

### Acceptance and stopping point

- A fresh upload produces one post and one default derivative job; the job
  is discoverable and `GET /api/jobs/{id}` returns status instead of 503.
- The job finishes with progress 100 and one ready variant; its long edge is
  at most 320 pixels, its URL loads, and the original hash is unchanged.
- Reprocessing the same work creates no duplicate job or variant.
- Interrupt a claimed job, restart, and observe recovery; exercise a missing
  original to verify retry/error reporting. Poll with a timeout.
- Hide the post and verify original and thumbnail denial; republish and
  verify visibility returns. Check that job status exposes only intended
  public fields and does not undermine moderation visibility.
- Run focused integration tests against real PostgreSQL for transaction and
  lease behavior, then server tests/vet, `make check`, and diff checks.

Stop after this integration slice and report exact files and evidence before
starting the live acceptance pass. No Elm redesign, new media formats,
broker, or deployment work is needed.

## 2. Exercise successful uploads and mutations live

Outcome: close the checks deliberately omitted from the previous audit.
Use a disposable PostgreSQL database and temporary media root with dedicated
fixtures. Preserve the existing development database and media.

1. Upload valid JPEG, PNG, and GIF fixtures. Verify persistence, dimensions,
   original bytes, search/detail results, and generated thumbnail/job status.
   Restart the server and confirm the saved data and media remain available.
2. Add/remove tags, inspect revision history, and revert a recorded tag edit.
   Submit one stale version and confirm a conflict leaves data unchanged.
3. Toggle favorite and change score; reload and verify both persisted values
   and `favorite:`/`score:` search behavior.
4. Add the uploaded posts to a collection, reorder, remove one, and confirm
   the returned order and membership after reload.
5. Exercise open local mode and configured bearer-token mode. With the token
   configured, verify missing/invalid credentials reject writes, valid
   credentials succeed, and audit records identify the system actor.
   This checks deployment-token behavior; session-based user login remains
   outside these two slices.
6. Hide/reject/republish fixture posts. Check search, detail, collection
   browsing, mutations, and direct original/thumbnail URLs for visibility
   consistency, with recorded moderation actor/reason/history.
7. Run a short browser flow: upload, select, edit tags/revert, favorite/score,
   collection browse, and reload. Verify the grid uses a ready thumbnail and
   Loupe can load the original. Record headless browser evidence separately
   from any native desktop/manual evidence.

Append the new results to the acceptance audit with commands, fixture IDs,
PASS/FAIL/BLOCKED status, and any unavailable checks. Record runtime cleanup.
If a defect appears, report the reproducer and a bounded repair scope; do
not label that check passed. Stop after one pass and review the results.
