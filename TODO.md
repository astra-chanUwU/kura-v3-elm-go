# Kura V3 implementation backlog

Use Grok 4.7 High in Cursor for the implementation slices below when available. Work in order, one slice at a time. Use the contracts below as the starting scope and resolve routine implementation choices autonomously. Stop only for an unproved safety guarantee, missing access, or an incompatible shared contract; report the concrete issue. Do not wait for confirmation between successful slices. Keep work reviewable on an isolated checkout and a `codex/<slice>` branch. Do not merge or push to `main`; Codex reviews and integrates accepted work.

## Shared boundaries

- Start each slice from the latest `origin/main`; report its starting commit and inspect `git status` and applicable `AGENTS.md` instructions. Do not reset, clean, stash, or overwrite another checkout.
- Preserve `docs/frontend-design.md` and `docs/design-concepts/` untouched. Do not inspect, print, commit, or use real library images, secrets, credentials, or a development database for proofs. Use generated fixtures and uniquely named disposable databases with existing guards.
- Keep the product's current Go/Elm/PostgreSQL boundaries. Avoid a new service, queue framework, or dependency unless the implementation evidence requires it. Do not expand `dhash-v1` into semantic search.
- Never put real passwords or tokens in command arguments, stdout, logs, fixtures, or persisted browser state. Generated test credentials are allowed in disposable test fixtures. Do not log database URLs. Preserve production data and existing runtime media.
- Give every slice a hard stop: implement only its accepted contract, run checks relevant to that slice, report skipped checks and blockers, commit only its scoped files, and provide the commit hash. Codex owns final review, integration, and acceptance.

## Ordered slices

### 1. Prove online-backup consistency (proof only)

**Evidence:** `scripts/library-backup.py` supports offline create/verify/restore; `server/internal/recovery` lists and verifies existing local backups; the Recovery panel does not create backups. The README requires stopping writers for a complete backup.

**Own:** focused test/harness files under `scripts/` or `server/internal/recovery/` only. Do not change product behavior.

**Contract and acceptance:** Trace the actual database and media publication/deletion order, including pending uploads, derivative/fingerprint jobs, Trash/restore, hidden posts, and orphan files. Decide whether a database snapshot and its referenced-media inventory can be copied consistently while writes continue; identify any exact bounded writer coordination needed. With generated media and a disposable database, exercise concurrent upload/job completion and prove each restored snapshot reference has the expected bytes and hashes. Interrupted copy, failed dump, missing media, and corrupt media must never produce a completed backup. Report dump/snapshot lifetime, manifest compatibility, atomic publication, failure recovery, and exact future interfaces/files. No online creation or restore implementation in this slice.

**Stop if:** the proof cannot establish consistency without broad writer pauses or format changes; report the evidence and smallest unresolved experiment instead of weakening the guarantee.

### 2. Create local backups online

**Depends on:** accepted slice 1 proof and frozen coordination contract.

**Own:** `server/internal/recovery/`, recovery API wiring and focused tests, plus only the minimum configuration needed for the fixed `KURA_BACKUP_ROOT`. Do not edit Elm in this slice.

**Contract and acceptance:** Admin-only asynchronous create/status/cancel behavior; session, role, cookie, and origin protections match existing private-library rules. One active creation per library across processes. Use opaque IDs, the configured local destination only, safe path/symlink handling, bounded time/resources, and a temporary directory that is atomically published only after dump, referenced-media copy, and manifest hashes succeed. Restart, cancellation, timeout, disk-full, and dump/copy failures stay visibly incomplete and recover safely. Existing `library-backup.py verify` and offline restore continue to accept successful output. S3 remains unsupported. Add focused failure and concurrency tests using disposable state.

**Stop if:** the format cannot remain backward-compatible or coordinated publication requires unsafe/unbounded pausing.

### 3. Finish the administrator Recovery workflow

**Depends on:** slice 2's frozen request/status contract.

**Own:** `web/src/Api/Recovery.elm`, `web/src/Domain/Recovery.elm`, `web/src/Feature/Recovery.elm`, the recovery entry point in `web/src/Page/Library.elm`, and focused Elm tests; do not touch global theme/preferences files.

**Contract and acceptance:** Admins can request a backup, see truthful queued/running/succeeded/failed/canceled states, poll only while work is active, cancel where supported, and verify a completed backup using the existing verifier. Refresh/reopen shows server-owned job state when available. Handle unavailable recovery, authorization loss, stale IDs, and failures accessibly. A backup is called complete only after server publication; the UI never offers online restore. Run Elm checks and a local browser pass when available.

**Stop if:** the API cannot distinguish verified completion from a request being accepted; keep the UI honest and report the missing contract.

### 4. Protect private libraries on S3-compatible storage

**Evidence:** private mode currently refuses S3 and persisted external media URLs because public object URLs bypass API cookie checks (`README.md`; `server/internal/media/s3.go`).

**Own:** `server/internal/media/`, media HTTP authorization/wiring, configuration, and focused tests; small UI/API adjustments only if the protected media URL contract requires them.

**Contract and acceptance:** First map every stored media reference to a validated internal object key using actual upload, derivative, and import writers. Serve private media through an authenticated same-origin handler that checks post visibility before opening the configured store. Preserve protected moderation previews and private caching rules. Never fetch arbitrary external URLs, follow redirects, or accept a browser-supplied object key. Prove viewer/admin access, hidden/rejected/deleted denial, traversal rejection, and S3-compatible store behavior with local fakes or a disposable endpoint. Public mode keeps its current behavior.

**Stop if:** persisted URL formats cannot be mapped to store-owned keys without fetching external URLs or migrating data; present the exact mapping gap and a bounded migration proposal.

### 5. Give the native CLI secure account sessions

**Evidence:** account login is browser-only; CLI bearer mode uses a deployment admin identity (`README.md`).

**Own:** `cli/cmd/kura/`, `cli/internal/client/`, and narrowly scoped auth/API files and tests. Avoid changes to browser session behavior.

**Contract and acceptance:** Add an explicit CLI login/logout flow for a named account and use its role-limited session for existing commands. Read passwords only from a terminal prompt or stdin; never accept them as flags/arguments or echo them. Persist session material only through an explicit opt-in in a user-private credential store/file with restrictive permissions; otherwise keep it in memory. Redact authorization and cookie values from all errors. Preserve existing deployment-token mode and make its precedence explicit. Test viewer/editor/admin authorization, expiry/revocation, logout, non-interactive input, and redaction.

**Stop if:** the current API cannot issue/revoke a CLI-appropriate account session without broadening capabilities; report the smallest server contract needed.

### 6. Make durable media-job failures diagnosable and recoverable

**Evidence:** thumbnail and fingerprint work is stored durably in PostgreSQL and retried after crashes; fingerprint failures need an explicit retry. Existing API exposes individual job status, while retry controls and operator diagnostics must be confirmed in the current implementation before adding anything.

**Own:** `server/internal/jobs/`, related focused HTTP handlers/tests, and only the relevant Elm upload/similarity status views if the audit confirms a missing user action. Keep ordinary user-facing failures concise and safe.

**Contract and acceptance:** First inventory current retry/backoff, attempts, error persistence, and visible status; do not duplicate existing controls. For a confirmed gap, expose bounded, authorized retry and enough sanitized failure state to tell an operator what action is needed. Enforce idempotency and per-job retry limits; never expose paths, credentials, or raw stack traces. Prove transient recovery, permanent failure, duplicate retry, and unauthorized access using generated media.

**Stop if:** the audit finds the requested behavior already exists or safe diagnosis needs a logging/telemetry redesign; report that evidence and do not add a parallel mechanism.

### 7. Measure and bound library performance at 100k records

**Evidence:** SQL benchmark scaffolding exists; ordinary limits and cursor pagination are already part of search, moderation, Trash, and similarity APIs. This slice establishes measured gaps before changing query behavior.

**Own:** generated benchmark/proof code and only the SQL/query files justified by its results; no real library data or images.

**Contract and acceptance:** Seed a uniquely disposable PostgreSQL database with deterministic synthetic posts, tags, moderation states, trash rows, and `dhash-v1` fingerprints. Measure representative first-page and later-page search, similarity, moderation, and Trash latency/query plans at 100k rows. Record hardware/database conditions and establish explicit budgets with Codex before optimization. If a budget fails, make the smallest index/query change, preserve stable cursor semantics and visibility filters, then rerun the same measurements. No speculative index set or product-wide benchmark framework.

**Stop if:** reproducible data or comparable measurements are unavailable; report setup and raw measurements without claiming a performance improvement.

### 8. Close targeted keyboard and narrow-screen workflow gaps

**Evidence:** the library already has keyboard shortcuts, Grid/Loupe/Compare/Survey, and responsive media browsing. This is a focused acceptance pass for workflows with concrete failures, not a visual redesign.

**Own:** only the affected Elm feature and its focused tests, after identifying the failing interaction. Coordinate with the current dark-mode/preferences/icons/CSS implementation before overlapping frontend files.

**Contract and acceptance:** Check search-to-selection, upload queue, moderation, and Recovery with keyboard-only navigation and a narrow viewport. A concrete fix must preserve focus, announce status/errors, provide reachable actions, and avoid clipping or hidden controls. Add a regression test for each fixed behavior and report browser/device coverage separately from automated evidence.

**Stop if:** the pass finds no reproducible blocker; return the short findings without changing UI.

### 9. Measure the actual browser at large-library scale

**Depends on:** slice 7's reproducible synthetic corpus. This complements its SQL/API measurements.

**Own:** a disposable browser fixture/harness and only frontend files justified by a measured regression. Coordinate with slice 8 and other frontend work.

**Contract and acceptance:** Exercise first load, repeated pagination, deep scrolling, selection, Compare return, query changes, and panel reopen against the synthetic 100k library. Measure visible DOM size, loaded-post retention, request count, and interaction delays; report browser and viewport conditions. Preserve the virtual grid, selection, query, and exact return position. Fix a demonstrated leak, redundant fetch, or unbounded retention with a bounded policy that preserves current behavior. Check that repeated use returns to a stable resource level. Keep synthetic media local and deterministic; do not scrape or use real images.

**Stop if:** browser instrumentation is unavailable; deliver a runnable fixture and explicit unverified checks instead of claiming browser performance from SQL timings.

### 10. Keep saved browser imports scoped to the current account

**Evidence to confirm first:** durable imports currently use a single IndexedDB checkpoint per browser origin, while Kura now supports multiple accounts. Inspect whether a second editor using the same browser profile can resume the previous account's picked files or receipts.

**Own:** the browser-import persistence adapter, import feature/state/context, relevant account-to-import wiring in `Page.Library`, and focused JS/Elm tests. Do not overlap slices 3, 8, or 9 in the same checkout.

**Contract and acceptance:** If the gap is reproduced, bind saved imports to a stable library target and account identity, never a bearer token, cookie, or password. Switching accounts must not show another account's file names, file blobs, or receipts, or replay its pending writes. Let the original owner explicitly resume its own checkpoint. Preserve open-local and legacy-token workflows, quota failure behavior, atomic checkpoints, and uncertain-write review. Handle older ownerless records explicitly without assigning them silently to whichever account signs in next. Prove account switching, logout/relogin, reload, and denied restore; keep credentials out of persistence. If current code already enforces this, report evidence and skip the implementation.

**Stop if:** a safe owner identity or migration cannot be determined without exposing credentials or silently adopting another user's files; report the missing contract.

## Practical execution lanes

Start with slice 1, then continue 2 → 3 after its proof passes. Slices 4, 5, 6, and 7 can each use independent branches and frozen API boundaries. Slice 9 follows 7. Only one worker should own shared frontend integration at a time across 3, 8, 9, and 10; queue them or agree disjoint files first. Do not run database-mutating suites against the same disposable database concurrently. Each successful slice gets its own scoped commit and a short result; Codex reviews the commits and runs final integration checks.

## Explicitly deferred

- Online restore or overwrite. Restore stays an explicit offline operation with the existing empty-target/new-media safeguards.
- S3 backup/restore, OAuth/passkeys, semantic or crop-aware image search, broad framework rewrites, and unmeasured performance work.
