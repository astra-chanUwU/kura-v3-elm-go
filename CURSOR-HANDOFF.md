# Cursor handoff: accept the existing feature-completion branch

Prepared 20 October 2026. Handoff publication branch: `main` at inspected
baseline `eb4844d`. Fetched candidate:
`origin/codex/complete-features` at `e2a9617`.
This handoff does not merge or certify that candidate.

## Paste into Cursor

Read CURSOR-HANDOFF.md and check live Ultra usage. Reconcile the existing
feature-completion branch in an isolated checkout, then prove a small set of
daily workflows and fix reproduced failures. Do not reimplement password
recovery, profiles, health tools or packaging that already exist in the candidate.
Keep Elm/Go/PostgreSQL and the current Library/Loupe design.

## Read first and preserve local work

Read README.md and TODO.md, but treat old gap labels as historical until compared
with current source. Review main...origin/codex/complete-features. The candidate
contains password change/offline reset, persisted profiles/avatar/lists,
collection routing, health/repair/preflight, CI and host packaging.

The original Mac checkout has unrelated uncommitted analytics changes in
Makefile, README.md, server/cmd/kura-web/main.go, web/index.html,
web/src/Page/Library.elm and new analytics sources/tests. It also has modified
TODO.md/docs/frontend-design.md and an untracked GROK-UI-HANDOFF.md.
These local-only changes are NOT transported by this documentation commit.
Do not reset, stage or assume the other PC has them. This handoff is self-contained
about the public-gallery correction below; do not depend on an untracked guide.

## K0. Reconcile and establish the candidate baseline

Create a work branch from pulled main containing this handoff, then review/merge
the fetched candidate into that branch. Do not merge or push implementation to
main. Inspect all applicable instructions, migrations and conflicts first.
Preserve import/request generations, optimistic versions, domain/module import
boundaries, roles, private-media checks and backup verification.

Use disposable database/storage. Run current candidate checks. A clean compile
is not browser acceptance; list checks actually run and unavailable dependencies.
Stop for usage checkpoint before K1.

## K1. Public viewing and authenticated contributions

Product requirement: unsigned visitors can enjoy the intended public gallery.
Uploads, edits, reactions and other contributions require a signed-in account
and the applicable role. Remove the legacy token-unlock UI from ordinary public
gallery use if it is still present. Preserve explicitly selected private mode.
Do not enable anonymous writes through the legacy open-local/system actor.

Acceptance: unsigned visitor browses/searches/opens public media; direct mutation
requests are denied; viewer/editor/admin controls match direct HTTP enforcement;
sign-in/out and account switches clear stale privileged state. Private-mode
catalog/media remain protected. Check desktop and phone browser navigation and
focus. Fix reproduced violations with regression coverage, then checkpoint usage.

## K2. Accept the shipped account/profile and recovery slices

Verify password change for every signed-in role, wrong-current-password failure,
session revocation and a generated sole-owner offline reset. Preserve last-admin
protection; never recover against the real owner's database.

Verify persisted display name/bio, avatar crop/save/cancel/removal and personal
lists with owner/visitor visibility. Reload, switch accounts and test expired
sessions, failed saves and conflicting edits. Keep server authorization and
private-list isolation. Check responsive compositions, keyboard/touch behavior
and narrow-screen reachable controls. UI presence does not prove persistence.

Use server/internal/{auth,profiles}, httpapi account/profile handlers,
cmd/kura-account, web/src/Feature/{Session,Profile}.elm and associated APIs/tests.
Fix only reproduced gaps; do not introduce OAuth/passkeys or a redesign.
Stop after this package and its usage checkpoint.

## K3. Optional next package after a fresh budget check

Pick ONE: collection Back/Forward/reload with surviving selection and nonzero
scroll; health scan/cancel/retry and explicit thumbnail repair; or packaged host
fresh setup/upgrade/backup verification. Use the candidate's existing tools,
fixtures and README. Do not claim 100k-library acceptance from a small fixture,
or a recoverable backup from a file's existence. Stop at that package's acceptance.

## Verification and stopping point

Run proportional targeted tests, then make check for integrated changes.
On the fetched candidate, make server-db-check requires an explicitly disposable
PostgreSQL test configuration; inspect scripts/ci-server-test.sh first.
Use make server-race for relevant server concurrency, make web-test for Elm/import
boundaries, and make package for packaging changes. Run git diff --check.
Database tests may truncate; never point them at the daily library.

Separate automated, HTTP, browser, package/restore and unavailable evidence.
Keep results in chat and lasting setup changes in README. This is bounded
acceptance and repair, not permission to rewrite the product or generate more
planning/completion Markdown.

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

## Working and delivery rules

Read applicable AGENTS.md and README, fetch first, inspect branch/status/diff,
and reproduce the current gap before editing. Use an isolated work branch or
checkout. Preserve existing uncommitted files, environments, databases, media
and previews. Use disposable fixtures/databases; never copy secrets through Git.
On Windows prefer WSL2 for the existing shell/Make workflows and follow the
current README; do not rewrite the stack to work around local setup.

Implement the packages below in order, checking usage between them. Commit only
the verified scope to the work branch; do not merge or push implementation to
main, deploy, publish releases or make real provider calls without the owner's
instruction. Keep progress in chat: behavior, commit, relevant checks, browser
evidence, unavailable checks and next unfinished step. Do not add completion
reports or expand the roadmap. Keep this file while its tasks remain; remove it
in a scoped cleanup commit only after completion or explicit rescoping.
