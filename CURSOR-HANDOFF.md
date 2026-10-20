# Cursor handoff: Kura: daily-library workflow and feature-completion acceptance

Expanded 20 October 2026. **24 ordered packages.** This file schedules work;
it does not claim the planned behavior is implemented or accepted.

Publication branch: `main` at `ac82c00`. Latest fetched feature candidate:
`origin/codex/complete-features` at `e2a9617`.
That candidate already contains password reset/change, persisted profiles,
avatars/lists, collection routes, health/repair/preflight, CI and host packaging.
Those are acceptance/repair tasks until an actual gap is reproduced.

Read README.md and TODO.md, then compare actual candidate source rather than
implement old 'confirmed gap' labels. Main's Mac checkout still has unrelated
uncommitted analytics, design and handoff changes; do not reset/stage them or
assume the other PC has them. This file independently states public reads and
authenticated contributions, so it does not require untracked GROK-UI-HANDOFF.md.
Keep Elm/Go/chi/PostgreSQL and current Library/Loupe compositions. The objective
is dependable management of the actual image library, using disposable fixtures
for all destructive acceptance.

## Paste into Cursor

Read CURSOR-HANDOFF.md completely. Check both live Ultra usage meters, then
continue through its ready task queue. Implement missing slices, verify and
repair existing workflows, commit each completed package and check usage before
continuing. Do not stop after the first package; honor the 95%/97% budget rules.

## Run the queue, not just the first task

The owner explicitly expanded this assignment on 20 October 2026. Continue
through READY packages while the shared quota check permits; do not stop merely
because the first package or the former short handoff is complete. This replaces
the earlier "stop after two/three packages" scope limit. Each package is a
checkpoint and reviewable commit, not a request for another permission.

- BUILD means implement the stated remaining slice after confirming the current
  source. VERIFY means exercise existing behavior and fix reproduced failures,
  not build another version or invent a defect. IF NEEDED requires the named
  evidence before changing it. GATED means perform safe local preparation only;
  leave missing owner inputs explicit and continue with independent ready work.
- Work in ID order, respecting dependencies. On the first pass, check whether
  each item is already satisfied, blocked or ready. Use existing tests/evidence;
  do not spend the allowance rewriting working features or rerunning unchanged
  full suites. Already-satisfied items need only proportionate current evidence.
- After a completed package, record its ID, commit, checks, remaining limitation
  and current usage briefly in chat, then take the next ready package. If blocked,
  name the exact dependency, leave the draft safe and move to a ready independent
  package. Stop when all ready work is exhausted, the quota/deadline requires it,
  or a required answer blocks all remaining work.
- Default implementation uses the Cursor Models pool. A separately assigned
  Other Models pass should review a named difficult change such as payment
  invariants, release configuration or authorization, with a small output and
  concrete regression requirements. Do not run two overlapping implementations.
- Keep an isolated work branch/checkout and disposable data. Fetch and inspect
  AGENTS.md/status/diffs first. Preserve existing changes, production identities,
  keys, environments, databases, uploads and running previews. On Windows use
  current project instructions and WSL2 for shell/Make where appropriate.
- Commit only verified scope to the work branch. Do not merge or push
  implementation to main, deploy, publish a release, buy services or make live
  payment/provider calls without the owner's instruction. Local simulators and
  generated fixtures are available for implementation and failure testing.
- Split a large package into small working slices when needed. Test meaningful
  behavior proportionately, then run integrated checks after shared-contract
  changes. A failed or unrun browser/native/provider check stays explicit.
  Never call source presence or HTTP 200 production acceptance.
- Keep progress in chat. Update permanent README/setup only where behavior
  changed; do not create new Markdown plans/completion reports. Keep this handoff
  while tasks remain and remove it only after completion or explicit rescoping.

## Ordered packages

### K01. Reconcile the feature-completion candidate [VERIFY]

Create a work branch from pulled main containing this file; inspect/merge origin/codex/complete-features there, preserving main's scoped changes.

**Done:** Migrations, generated artifacts and package scripts agree; run one current baseline and record real failures/unavailable dependencies. Do not duplicate existing password/profile/health/package implementations.

### K02. Make public gallery reads work signed out [VERIFY]

Own access middleware, API/media routes, Session and ordinary visitor chrome.

**Done:** Unsigned visitors browse/search/open public posts, profiles, lists and avatars without deployment-token unlock UI. Explicitly private mode remains protected. Do not remove auth configuration to simulate success.

### K03. Require authenticated, role-gated contributions [VERIFY]

Own SessionContext/PrivateAccess/write capability and direct mutation handlers.

**Done:** Anonymous upload/edit/reaction/collection mutations fail; viewer/editor/admin rules agree across UI and HTTP. Legacy open-local/system actor cannot accidentally authorize anonymous public-gallery writes.

### K04. Accept password change and sole-owner recovery [VERIFY]

Own auth service/store, accounts handlers and cmd/kura-account reset-password.

**Done:** Every signed-in role changes only its own password after current-password proof; sessions revoke. Generated sole owner recovers offline via stdin without rebootstrap, privilege changes or weakening last-admin protection.

### K05. Keep profile usernames separate from identity [VERIFY]

Own internal/profiles, route resolution and stable actor attribution.

**Done:** Case/normalization, reserved/conflicting names and deliberate rename preserve account ID, sessions, saved searches/history and creator identity. Old profile-link behavior is explicit; adopted local/system actors remain stable.

### K06. Persist complete profile editing [VERIFY]

Own Profile feature/API and profile storage/view mapping.

**Done:** Display name, bio, optional website and empty-field clearing persist across reload/second session. Safe URL/text handling, save/cancel, stale versions and failed-save draft recovery work for every signed-in role.

### K07. Finish avatar crop and upload behavior [VERIFY]

Own profiles/avatar.go and actual crop/preview UI.

**Done:** Bounded JPEG/PNG/static-WebP crop/replace/remove works on desktop/touch; malformed/animated/oversized uploads fail. Failed/cancelled replacement retains old image, metadata is stripped and cache refresh shows the new asset.

### K08. Include avatars in backup/visibility boundaries [VERIFY]

Own profile asset storage, backup inventory/restore and profile media routes.

**Done:** Public/private account modes expose only eligible avatars; backup/restore retains matching asset bytes. Avatar removal never deletes library originals or creates an unintended gallery post.

### K09. Prove user list ownership and attribution [VERIFY]

Own profile_lists store/HTTP and Profile/Collections features.

**Done:** New list records use actual authenticated stable creator, not a client field; unknown legacy collections stay shared/legacy. Reordering/renaming preserves creator and current collaboration permissions.

### K10. Keep list previews and counts visibility-safe [VERIFY]

Own list cover/count queries, profile pagination and media predicates.

**Done:** Hidden/trashed/private media cannot leak through count, cover or direct list/profile links. Two accounts and signed-out/private-mode fixtures verify empty, missing/disabled account and deleted-list behavior.

### K11. Accept collection URL and history routing [VERIFY]

Own App/Route.elm, Collections state and collection request generations.

**Done:** Collection -> Loupe -> Back, collection -> search -> Back, direct link, reload and private sign-in return restore correct collection. Deleted/missing/revoked collections recover clearly without stale search results.

### K12. Preserve active selection and real scroll return [VERIFY]

Own Page/Library, navigation modes, MediaGrid and request sequencing.

**Done:** Across multi-page results, Inspector/Loupe/Compare/Survey return preserves surviving selection and nonzero scroll. Browser history, resize and late network responses do not jump focus or reapply stale state.

### K13. Prove reload-resumable browser import [VERIFY]

Own browser-import.js, Feature/Import and receipt reconciliation.

**Done:** Pick files/folder, cancel, reload and explicitly resume using fixtures; quota/storage/unsupported input and uncertain writes recover without duplicate attachments. Never move/delete source files or silently resume after account change.

### K14. Resolve optimistic metadata/tag conflicts [VERIFY]

Own Metadata/BulkMetadata/Tags/TagManagement and matching HTTP/domain owners.

**Done:** Concurrent tag/artist/source/rating edits preserve drafts and expose retry/reload choices; rename/merge/history/revert maintain references and role checks across bulk operations.

### K15. Accept duplicate review, moderation and Trash [VERIFY]

Own Duplicates/similarity, moderation routes and Trash feature.

**Done:** Exact-copy and perceptual review keep scope honest; moderate/trash/restore fixtures with protected previews and history. Selection, collections and visibility remain correct after restore; no silent permanent deletion.

### K16. Verify ZIP export and CLI/browser agreement [VERIFY]

Own export jobs/HTTP, CLI commands and existing parity fixtures.

**Done:** Requested original bytes match archive contents and visibility/limits; cancel/retry/expired access recover usefully. Relevant CLI and browser paths enforce the same role and ownership contracts.

### K17. Accept bounded health inventory and cancellation [VERIFY]

Own internal/health, HTTP health handlers and Feature/Health.

**Done:** Missing originals/derivatives, failed backlog, unknown storage and latest verified backup display accurately. Explicit scans are bounded/cancelable and rediscovered after reload; cheap /health stays independent.

### K18. Prove thumbnail repair durability [VERIFY]

Own health repair and existing durable jobs/store; leave originals immutable.

**Done:** Duplicate repair coalesces; corrupt/missing derivative retries, crash/restart and cancellation keep accurate progress with bounded attempts. Original hashes stay unchanged and unauthorized repair fails.

### K19. Boot the actual release package [VERIFY]

Own scripts/package-release.sh, server/web/account/CLI binaries, migrations and bundled scripts.

**Done:** On one available host target, clean package boots without source checkout, resolves all assets/tools/checksums and supports account setup. Claim additional OS/architecture only after actual builds/boots.

### K20. Accept migration/preflight and upgrades [VERIFY]

Own internal/preflight, server startup/resource limits and existing upgrade instructions.

**Done:** Existing generated library upgrades and restarts without losing data; incompatible schema, missing DB/tools and unwritable paths report actionable sanitized errors. Limits do not break supported uploads/video/ZIP streams.

### K21. Run backup lifecycle and restore proof [VERIFY]

Reuse library-backup.py, make library-drill and existing lifecycle drill with explicitly disposable PostgreSQL/storage.

**Done:** Create/verify/cancel/discover backup, process crash, replica retention and new-environment restore retain records/originals/avatars. Missing or corrupt manifest/data fails; a directory's existence is not recovery evidence.

### K22. Measure a real large-library browser [VERIFY]

Reuse library-perf-100k.py and generated fixtures; browser_scale_100k.py geometry alone is not measured browser evidence.

**Done:** Record hardware/browser, first usable grid, search, deep scroll, mounted cells, interaction delay and retained memory with 100k rows/useful collections. Keep raw measurements ignored/temp; no extra Markdown report.

### K23. Fix measured retention and interaction bottlenecks [IF NEEDED]

Own Domain.Sequence/feature pagination, virtualized grid and measured query paths; use K22 evidence first.

**Done:** Address observed stalls or growth with before/after measurements and navigation correctness. Distinguish mounted DOM size from retained sequence pages; do not redesign pagination or claim bounded memory without evidence.

### K24. Finish CI and device-class daily acceptance [VERIFY]

Own existing workflow, disposable DB scripts and relevant UI tests. Preserve mechanically checked Elm import boundaries.

**Done:** Intended database/race/backup tests execute rather than silently skip; hosted/local results are distinct. Run keyboard/touch and phone/tablet/desktop profile/library flows, fix reproduced regressions and update practical README instructions. No OAuth, feeds, messaging or speculative infrastructure.

## Project checks

On the integrated candidate use make check, make web-test, relevant server
tests and make server-race. Inspect scripts/ci-server-test.sh before using
make server-db-check: explicitly disposable DB configuration is required.
Use make package and the existing lifecycle/restore drill only for affected
contracts. Never run destructive integration helpers against the daily library.
Always run git diff --check before committing. Keep automated, HTTP/database,
actual browser, native/package and unavailable evidence distinct.

## Shared Ultra budget: check between milestones

Owner-supplied snapshot on 20 October 2026: Cursor Models 75% used, Other Models
91% used. These are two account-wide pools shared by every repo and PC. The
snapshot and the earlier "20 hours remaining" are historical, not a live meter
or a fresh countdown. Read the actual reset time on the working PC.

- Target 97% USED in each pool; 98% is the owner's optional upper target, never
  permission to overshoot. At the supplied snapshot, the room to 97% was 22
  percentage points for Cursor Models and 6 for Other Models.
- Default implementation lane: explicitly select Cursor Grok 4.5 or Composer
  2.5 in the editor, if the dashboard attributes them to Cursor Models. Reserve
  Other Models for a named, bounded diagnosis/review assignment. Instructions
  in this file cannot change the editor's selected model.
- Use one account-wide coordinator and, by default, one active worker. Do not
  silently start subagents, background agents, or another model. Another PC's
  requests also consume the same pools; coordinate before continuing.
- Before the first milestone, before each subsequent milestone, and immediately
  after each milestone: refresh the authenticated Cursor Spending dashboard
  (https://cursor.com/dashboard/spending) through an available browser/computer
  tool. Read BOTH labels, used percentages, reset time and on-demand state.
  Record a short timestamped usage line in chat. Do not extract browser cookies,
  copy login tokens into scripts, or commit account data.
- If browser tools cannot read the signed-in dashboard, ask the owner for a
  fresh reading of both meters and stop at that checkpoint until it arrives.
  A repo agent does not automatically have access to the editor's billing UI.
  Never invent usage or estimate pool percentage from tokens, request counts,
  the $200 subscription price, or another provider's usage meter.
- Below 95% in the assigned pool, finish one bounded package and its relevant
  checks, then recheck. At 95% or above, stop long/concurrent work and use only
  short, individually checked steps. At 97% or above, start no further work
  on that pool. Do not switch pools, enable on-demand billing, buy credits,
  or continue across a reset without the owner's direction.
- Checkpoints cannot interrupt a request already in flight or guarantee an
  exact percentage. If a step could consume the remaining margin, stop early.
  Keep on-demand usage OFF; have the owner confirm its actual dashboard state.
  When a limit or deadline approaches, save the current files, report any
  incomplete validation and the next step, and pause without further requests.

Cursor hooks can gate new prompts or control continuation, but they still need
a trusted usage source. Cursor's documented usage/spending API is a team Admin
API; this individual Ultra account has no verified public quota endpoint here.
No hook, usage reader or automatic 97% cutoff is installed by this handoff.
A browser checkpoint is the initial mechanism. A future user-level gate on the
working PC should share one fresh account snapshot across repos, reject missing
or stale data, and block new prompts/continuation at the threshold. A stop hook
alone does not cancel in-flight model work.

Sources checked 20 October 2026:
[usage](https://cursor.com/help/models-and-usage/usage-limits),
[hooks](https://cursor.com/docs/hooks),
[team Admin API](https://cursor.com/docs/account/teams/admin-api).
