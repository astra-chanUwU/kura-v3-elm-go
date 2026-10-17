# Grok handoff: finish the current Kura feature set

Prepared 2026-10-17 from local HEAD `90a1340` and the working tree.
This replaces the previous task list. Recheck the code before starting; some
work may have landed since this snapshot.

## Objective and working rules

Make the current product complete enough that **I can safely manage my actual
image library here every day**. Finish existing workflows, recovery paths,
complete user profiles with their lists, and installation/upgrade support.
Keep Elm, Go/Chi, PostgreSQL, and the current
Library/Loupe visual language. Avoid redesigns and open-ended feature expansion.

Work through the numbered items in order, with scoped implementation commits.
Confirm each gap before changing code. Reuse working implementations. Report a
concrete blocker when acceptance cannot be established; UI presence and unit
tests alone do not prove a workflow is complete.

- Inspect Git status, branch, applicable `AGENTS.md`, and tests. Use a `codex/`
  branch or isolated checkout when needed; never reset/clean another checkout.
  Do not merge or push to main without a separate instruction.
- There are unrelated uncommitted analytics changes in `Makefile`, `README.md`,
  `server/cmd/kura-web/main.go`, `web/index.html`, `web/src/Page/Library.elm`,
  and new analytics source/test files. `docs/frontend-design.md` is also modified.
  Preserve them; inspect overlaps and never stage unrelated hunks.
- Preserve design concepts, running previews, `.env.local`, `.local/`, real
  media, and the development database. Use generated fixtures and explicitly
  disposable databases/storage. Database tests truncate tables.
- Reuse optimistic versions, access/request generations, import receipts,
  durable jobs, media authorization, roles, and backup verification.
- Keep credentials out of arguments, logs, URLs, checkpoints, and commits.
- Keep progress/results in chat. Do not add more planning/completion Markdown.
  Update the README only for behavior users/operators need to understand.

## Already present: finish and verify, do not rebuild

Source inspection shows these implementations exist. This is not a fresh
runtime acceptance claim:

- Grid/Loupe/Compare/Survey, keyboard navigation, themes, responsive panels.
  Survey now requires at least two selected images.
- KuraQL, saved searches, paginated ordered collections, rename/delete/move.
- Tags/history/revert, tag rename/merge, favorites/scores, content ratings,
  source/artist editing, and bulk metadata/rating changes.
- Uploads/thumbnails, browser folder import with optional reload persistence,
  receipt reconciliation, and the resumable Python folder importer.
- Exact-copy review, perceptual similarity/index jobs, browser moderation with
  protected previews/history, Trash/restore, and ZIP exports.
- Private libraries, viewer/editor/admin accounts, account management,
  invitation signup, browser sessions, and CLI account login.
- JPEG/PNG/GIF/static WebP and optional FFmpeg support for static AVIF,
  animated WebP, MP4, and WebM. Do not list all extended media as missing.
- Browser local-backup creation/verification/cancellation/discovery, scheduled
  backups, verified replica copies/retention, and offline restore tools.

## 1. Finish the existing daily workflows

**Status: acceptance work. Reproduce failures before calling them bugs.**

Inspect `web/src/Page/Library.elm`, relevant `web/src/Feature/` modules,
`web/browser-import.js`, and the matching HTTP/domain code.

Use a small disposable private library and actual browser interactions:

- Sign in, accept an invitation, switch accounts, sign out, and revoke/expire a
  session. Check viewer/editor/admin controls and direct HTTP denials.
- Pick real fixture files through Upload; import a folder; reload and explicitly
  resume a saved import. Check cancellation, quota/storage failure, unsupported
  media, duplicate collection attachment, and uncertain write outcomes.
- Search/save a search; organize a multi-page collection; edit tags, metadata,
  ratings, and reactions; resolve an optimistic conflict without losing drafts.
- Enter/exit Loupe, Compare, Survey, exact-copy review, and similarity review.
  Return to the same search/collection with surviving selection and genuinely
  nonzero scroll. Check video posters/playback as well as images.
- Moderate, Trash/restore, and export fixture posts; verify original bytes,
  visibility, and collection behavior afterward.
- Create/verify a backup in Recovery, refresh during work, and cancel a request.
  Reuse the existing lifecycle drill for process/replication cases.
- Exercise keyboard-only use and a narrow viewport: reachable controls, focus
  return, Escape behavior, readable errors, and no overlapping panels.

Fix reproduced dead ends: stuck loading, inaccessible actions, stale responses
following navigation/account changes, silent truncation, missing retry or
reconciliation, and discarded drafts. Keep intentional operation limits visible.

**Done:** the small end-to-end pass succeeds and fixes have proportional
regression coverage. List unavailable browser checks honestly. Existing import,
persistence, batch, and export limits do not require unlimited replacements.

## 2. Complete account maintenance and owner recovery

**Confirmed gap:** the current auth routes offer sessions, invitations, and admin
user updates, but no dedicated self-service password-change endpoint/UI.
`server/cmd/kura-account/main.go` only supports initial `bootstrap`, which refuses
another enabled login admin. An existing admin can reset other accounts, but a
locked-out sole owner needs a supported recovery path.

Own `server/internal/auth/`, account HTTP handlers, `Feature.Session`, its API
module, and `server/cmd/kura-account/`.

- Add Change password for the signed-in account, available to every role.
  Verify the current password, reuse password policy, and atomically revoke
  sessions after success. Clearly require signing in again.
- Add an explicit offline password-reset subcommand for an existing account,
  using operator/database access and stdin for the password. Never reopen
  bootstrap, create an unexpected admin, or weaken last-admin protection.
- Clear password drafts after submission/close/access changes. Handle invalid
  passwords, expired sessions, and ambiguous responses without automatic retries.
- Document sole-owner recovery in the README. Email, OAuth, public password
  reset, and passkeys are outside this task.

**Done:** a viewer changes their own password without admin rights; old
credentials/sessions stop working; another account cannot be changed through
self-service; and a generated sole owner can recover offline.

## 3. Ship complete, polished user profiles and personal lists

**Explicit user requirement:** users need a profile picture, username, optional
website, and an X-style profile page where people can see their lists. Deliver
the full persisted feature, including editing and real browser acceptance, not
a static profile mockup or an account form without a working profile page.

**Confirmed gaps:** accounts currently store a login handle but have no profile
fields/routes/page. Collections currently have no creator/owner attribution;
`CreateCollection` only receives a name. Do not infer ownership from whichever
user happens to be signed in when collections are read.

Own additive migrations, profile contracts/storage/API, profile media, Elm
profile/edit views, identity links, and collection creator attribution. Reuse
the account/session identity and existing collections; do not build another
unrelated lists system. Coordinate route work with item 4.

### Profile page and editing

- Provide a stable, directly addressable profile URL, for example `/u/USERNAME`.
  Support reload, copied links, Back/Forward, and private-library sign-in return.
- Show a prominent circular profile picture, display name, `@username`, optional
  short bio and website, and a paginated Lists section. Use a restrained profile
  header inspired by X while retaining Kura typography, spacing, and themes.
- Offer **My profile** from the account menu. Account names/avatars should link
  to profiles wherever an existing real account attribution is displayed.
- Owners get **Edit profile** with display name, username, bio, optional website,
  and upload/replace/remove picture. Provide a useful initial avatar, validation,
  save/cancel, pending/error states, and an unsaved-changes guard. Persist changes
  on the server and show them consistently after reload and in a second session.
- Every signed-in role can edit its own profile. A viewer's self-profile changes
  must not grant library writes; account A cannot edit B's profile or avatar.
  Handle stale edits with optimistic versions and preserve the user's draft.
- Define username lengths, allowed characters, normalization/uniqueness, and
  reserved names. Support deliberate username changes with atomic conflict
  checking and clear old-link behavior. Preserve account ID, sessions, creator
  attribution, saved searches, and history. Existing `ActorIDForUser` treats
  adopted `local`/`system` handles specially: decouple mutable profile usernames
  from that stable identity rather than accidentally changing actor keys.
- Accept an empty website or a validated HTTP(S) URL; render user text as text
  and open website links safely. Do not fetch website content on the server.

### Profile pictures

- Use a bounded JPEG/PNG/static-WebP upload with a square crop/preview. Validate
  actual decoded content and dimensions, normalize to a bounded image, and strip
  embedded metadata. Reject SVG, executable content, oversized/malformed files,
  and unsupported animation with a useful error.
- Store avatars as profile assets through the media abstraction, separately
  from library posts. Replacing/removing one must not create library posts or
  delete shared originals. Avoid stale avatar caching after replacement.
- Preserve private-library authentication for profile/avatar reads and prevent
  alternate static URLs bypassing it. No arbitrary external avatar hotlinks.
- Include profile data and local avatar assets in backup/restore verification,
  release packaging, and applicable health checks. Failed uploads must not
  destroy the previous working avatar.

### A user's lists

- Label existing collections **Lists** within the profile UI. Stamp the actual
  authenticated creator's stable account ID on new collections at creation;
  never trust a client-supplied owner or change ownership on ordinary edits.
- Show only that user's attributed lists on their profile, with name, a small
  visible-media cover, visible-item count, and pagination. Opening a list uses
  existing collection browsing, ordering, and return navigation.
- Preserve existing collections as legacy/shared when their creator is unknown.
  Do not assign all existing lists to the first/newest/admin account or expose
  every shared collection as if it belongs to every user.
- Profile list attribution does not silently change current collaborative edit
  permissions. Preserve viewer/editor/admin rules and name the creator clearly.
  Owners with existing collection-write permission can create/manage lists from
  their profile; viewers can browse and edit their profile without gaining it.
- Honor library access and post visibility for list pages, covers, and counts.
  Do not expose hidden/trashed media, private saved searches, or other users'
  information through profile previews. Profiles stay within the current
  library visibility boundary; no anonymous public-sharing system is implied.
- Make empty profiles useful: owners see a create-list action when permitted;
  visitors see a clear empty state. Handle deleted lists, disabled/missing
  accounts, loading failures, and session changes without stale private data.

**Done:** two generated accounts can have distinct profiles and lists; self
editing, avatar crop/replace/remove, optional-field clearing, username conflicts
and changes, direct links/reload, pagination, list navigation, permissions,
disabled accounts, private avatar access, and backup/restore are verified.
Prove both the HTTP contracts and actual desktop/narrow browser flows. Check
keyboard focus, labels, avatar fallback, light/dark layouts, and error recovery.
No placeholder tabs, dead actions, fake counts, or frontend-only saved fields.
Followers, feeds, messaging, and social counters are outside this feature.

## 4. Make collection navigation survive reload and history

**Confirmed gap:** `web/src/App/Route.elm` serializes only query, post, and
Grid/Loupe view. Collection browsing lives separately in Library/Collections
state, so URLs cannot identify the active collection workspace.

- Add a bounded, validated collection identity to routes. Reuse collection
  browsing/pagination and preserve existing search/Loupe links.
- Make opening a collection, Back/Forward, reload, copied links, and private
  sign-in return resolve to that collection. Define search-versus-collection
  precedence and keep the Navigator consistent.
- Handle deleted/missing collections and revoked access clearly. Invalidate
  stale requests when changing result source.
- Preserve in-session selection/scroll return. Do not persist tokens or encode
  every selected ID/cursor in URLs. Shared Compare/Survey selections are outside
  this task.

**Done:** collection → Loupe → Back, collection → search → Back, reload, direct
links, and sign-in return show the intended workspace, including collections
larger than one page.

## 5. Add library health and explicit derived-media repair

**Confirmed gap:** job-status and similarity-index endpoints exist, but there is
no library health inventory/panel or general thumbnail-repair workflow.

- Add a compact admin panel and small service/API for bounded counts/pages of
  missing originals, missing/failed thumbnails, processing backlog, and latest
  verified backup. Label storage usage unknown when not measured.
- Make expensive local-storage scans explicit, asynchronous, bounded, and
  cancelable. Keep `/health` as cheap liveness.
- Add explicit thumbnail regeneration/retry through existing durable jobs.
  Coalesce duplicate repair requests, bound retries, show progress, and
  rediscover active work after reload.
- Never rewrite/delete originals, automatically remove orphans, or expose
  server paths, object keys, credentials, or raw worker errors. Support local
  storage first and label unsupported S3 checks honestly.

**Done:** missing-original, corrupt/missing-thumbnail, failed-job, duplicate
repair, cancellation, reload, and role-denial fixtures produce accurate results;
derived-media repair leaves originals unchanged.

## 6. Finish installation, upgrades, and configuration checks

**Confirmed gap:** there are development build/run commands but no release
packaging/preflight entry point. The API's `http.Server` has no explicit
resource/time limits; the frontend server already sets several limits.

- Package server, frontend server, account tool, CLI, built frontend assets,
  migrations, and backup scripts with checksums. Start with one tested host
  target; claim additional platforms only when verified.
- Provide reproducible build/run instructions and an upgrade procedure using
  the current migration strategy. Never seed automatically. If adding migration
  tracking, explicitly support libraries that already applied migrations.
- Add read-only preflight for config, database connectivity/schema compatibility,
  storage/backup roots, Python/PostgreSQL backup tools, and optional FFmpeg.
  Sanitize output and distinguish optional-feature availability from failures.
- Add bounded dependency readiness separately from liveness. Set deliberate
  API limits without breaking slow uploads, video ranges, or ZIP streaming.
- Ensure packaged backups find their scripts outside the source checkout.

**Done:** a generated library installs, survives restart, upgrades without losing
data, and completes backup/restore using the package. Missing tools, incompatible
schema, unwritable storage, and unavailable database give actionable errors.
Record tested platforms and preserve existing user configuration/data.

## 7. Establish real large-library browser acceptance

**Confirmed evidence gap:** `scripts/browser_scale_100k.py` is a geometry model;
it explicitly returns `browser_measured: false`. SQL/model output does not
establish interactive performance.

- Reuse `scripts/library-perf-100k.py` and generated disposable fixtures. Measure
  a real browser against 100,000 rows and a useful multi-page collection.
- Record first usable grid, search completion, repeated/deep scrolling, mounted
  cells, interaction delay, and retained JS memory after pagination and query
  changes. State hardware/browser and number of loaded pages.
- Exercise Inspector, selection, Compare/Survey, and scroll return. Verify the
  DOM stays windowed; measure sequence retention separately from DOM size.
- Fix measured bottlenecks locally. Establish concrete budgets from measurements;
  do not claim bounded total memory while `Domain.Sequence.append` retains all
  fetched pages.

**Done:** reproducible browser measurements exist, observed stalls/leaks are
addressed, and remaining practical limits are disclosed. Keep machine results
in ignored/temp output; do not add a Markdown performance report.

## 8. Keep the completed behavior checked in CI

**Confirmed gap:** this checkout has no `.github/workflows/` directory.

- Add push/PR checks for `make check`, appropriate race checks, and serial
  PostgreSQL-backed tests against a migrated disposable database. Explicitly
  set `KURA_TEST_DATABASE_URL`; intended database tests must not silently skip.
- Match `server/go.mod`, `cli/go.mod`, and `web/elm.json`. Verify toolchain
  availability before selecting installers/runners; never silently downgrade.
- Add a separate generated-fixture backup/restore proof using compatible
  PostgreSQL tools. Reuse existing drills where appropriate. Require no user
  secrets, development database, or real media. Clean up on failure.
- Keep timing-sensitive scale checks out of CI gates until budgets exist.
  Distinguish local-equivalent commands from actual hosted runs.

**Done:** workflow syntax and local equivalents pass with intended integration
coverage. A hosted run must pass once publication is separately authorized;
report it as pending until then, rather than claiming hosted acceptance.

## Separate follow-ups, outside current-feature completion

- Watched-folder polling import. The existing Python importer is one-shot and
  resumable. If requested separately, reuse receipts and stable-file checks,
  preview before applying, and never move/delete sources.
- Remote offsite replication, S3 backup/restore, and online restore. Local backup
  scheduling/replicas already exist; a same-machine directory is not protection
  against machine loss.
- Real remote S3 deployment acceptance. The existing live storage test permits
  only a disposable loopback endpoint and cannot prove production acceptance.
- Semantic/crop/rotation-aware similarity, animated AVIF, OAuth/passkeys,
  email delivery, per-collection ACLs, and unlimited import/export sizes.

Do not implement these just to make a broader “feature complete” claim.

## Verification and delivery

Run `make check`, `git diff --check`, and focused tests for changed contracts.
`make library-drill` requires `KURA_BACKUP_TEST_ADMIN_URL` pointing to explicitly
disposable local PostgreSQL; server database tests require
`KURA_TEST_DATABASE_URL`. Neither may target the development library.

The existing backup lifecycle proof is
`python3 scripts/backup-lifecycle-drill.py --run`; it needs Docker and creates
its own resources. Run it when changes affect that contract. Use actual browser
flows for UX claims; Node/Elm models are not browser acceptance.

Deliver in chat: completed items and scoped commit hashes, remaining blockers,
and separate automated, HTTP/database, browser, packaged-artifact, and
unavailable evidence. Do not claim production readiness from local tests alone.

## Required cleanup: delete this file after committing the work

This is a temporary handoff, not permanent repository documentation.

1. Finish and verify the work, then commit only implementation scope. Keep this
   handoff out of feature commits; never use `git add .`.
2. After the final implementation commit, delete **this exact `TODO.md`**.
   It is currently tracked: use `git rm -- TODO.md` and make a small cleanup
   commit such as `Remove completed Grok handoff`. If it is untracked in your
   checkout, remove it without creating an empty commit.
3. Verify the file is absent from the final tree and no new plan/report Markdown
   remains. Check Git status; preserve unrelated existing changes instead of
   resetting/deleting/staging them to manufacture a clean status.
4. If blocked with unfinished work, keep the handoff until the user takes over
   and report exact outstanding items. Do not erase the only task list while
   claiming completion.

Do not delete other Markdown files or rewrite Git history for cleanup.
