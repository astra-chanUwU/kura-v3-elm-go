# Kura: next implementation tasks

Base: `8515f24` or newer `origin/main`. The previous ten feature branches have been integrated and reviewed. This is the next handoff, not a claim that every previous acceptance item is complete: actual 100k-library browser measurements are still pending.

The user is manually checking the dev preview. Codex supervises integration and tests; use Grok 4.7 High in Cursor for tasks 2, 3, 5, and 6 below. Task 4 belongs to Codex and Luna. Work autonomously within each accepted contract, one scoped branch/commit per task; do not merge or push to main. Report evidence and exact blockers instead of weakening a guarantee.

## Shared rules

- Start from current `origin/main` on `codex/<task>`; inspect the real checkout and applicable `AGENTS.md`. Never reset or clean another checkout.
- Preserve `docs/frontend-design.md` and `docs/design-concepts/`. Leave running dev services, the development database, real media, `.env.local`, and `.local/` alone. Tests use generated files/accounts and uniquely disposable databases/containers.
- Keep Go/Elm/PostgreSQL ownership and current UI language. Reuse jobs, authorization, import receipts, and backup verification. Avoid new services or generic frameworks.
- Do not print credentials or URLs containing them, put passwords in arguments, persist tokens in browser checkpoints, or commit runtime data. Use stdin/environment/private fixture files.
- Run relevant tests, report skipped checks honestly, and provide scoped commit hashes. Codex runs final database, Docker, and browser acceptance. Stop for an unproved data-safety guarantee or missing contract; resolve routine choices yourself.

## 1. Manual browser acceptance — user / Codex

The user is testing the private dev preview. Cover actual file picking/upload, reload/resume, account switching, viewer/editor/admin permissions, keyboard/narrow viewports, selection, Compare and exact scroll return. Record reproducible issues; do not claim browser acceptance from Elm/Node checks. Large-library browser timing/retention measurement remains separate.

## 2. Continuous integration — Grok, first

Own `.github/workflows/`, minimal test setup helpers, and necessary Makefile/README changes. Add checks on pushes and pull requests using versions actually required by `server/go.mod`, `cli/go.mod`, and `web/elm.json`. Verify toolchain availability before selecting action/install versions; do not silently downgrade Go or Elm.

Run `make check`, CLI/server race checks, and a serial PostgreSQL-backed server suite against a newly created, migrated disposable database. Set `KURA_TEST_DATABASE_URL` explicitly so database tests cannot silently skip. Add a separate guarded backup create/restore proof job with Python and matching PostgreSQL tools; jobs must clean up on failure and never need repository secrets or a development database. Keep SQL/browser performance measurements out of timing-sensitive pass/fail gates until budgets exist. Verify workflow syntax and run the equivalent commands locally; distinguish a real hosted run from local simulation.

## 3. Library health and repair — Grok, after CI

Own a small admin health service/API plus one Elm panel and focused tests. Inventory existing jobs/retry/status paths first. Show bounded counts/pages for missing originals, missing/failed thumbnails, processing backlog, storage usage (unknown when not cheaply measurable), and latest verified backup. A slow storage scan must be explicit, asynchronous, bounded/cancelable, and separate from `/health` liveness. Never expose server paths, object keys, raw worker errors, or credentials.

Keep the first pass read-only except explicit derived-media repair. Repair uses existing durable job/idempotency rules with authorization and retry bounds; never rewrite/delete originals or automatically remove orphans. Support local media first; report unsupported S3 checks honestly. Prove missing/corrupt/generated fixtures, denied viewers/editors, duplicate repair requests, cancellation, and safe errors. Coordinate Recovery changes with task 4 rather than editing its files concurrently.

## 4. Backup lifecycle — Codex + Luna, implemented and verified

Implemented opt-in KURA_BACKUP_INTERVAL (minimum 1h), KURA_BACKUP_KEEP, KURA_BACKUP_REPLICA_ROOT, active_job in the admin backup listing, and safe maintenance_error warnings. The Docker drill (`python3 scripts/backup-lifecycle-drill.py --run`) passed two-process locks/discovery/cancellation, graceful shutdown, abrupt restart, >16 MiB real PNG upload/copy/restore, retention, replica-storage failure, and corruption rejection. Clock-controlled Go tests cover scheduling. Retention uses atomic quarantine and preserves unknown/invalid/changed data. Development scheduling remains disabled; the manual preview was not interrupted. A second local directory simulates another disk, not offsite durability. Remote S3/SFTP backup, calendar-driven restore drills, and online restore remain separate work.

Use this accepted contract when packaging configuration. Coordinate any further Recovery file changes instead of overlapping an active worker.

## 5. Packaging and preflight — Grok, after task 4 contract

Provide a reproducible release containing the server, CLI, built Elm/JS/CSS, and backup scripts. Pin compatible toolchains; build a Linux/macOS target first and cross-compile other targets only when support is real. Include checksums and a practical startup/upgrade procedure that preserves database/media; no automatic seeding or destructive migrations.

Add an operator preflight command for configuration, database connectivity/migration state, writable storage/backup roots, required backup tools, and optional FFmpeg. Keep secrets out of output. Separate liveness from bounded dependency readiness; cover valid, missing-tool, unwritable-root, unavailable-database, and optional-feature-disabled cases. Set deliberate HTTP resource/time limits without breaking large uploads or streamed media/export responses. Codex tests the packaged artifact and failure cases in disposable containers here.

## 6. Watched-folder import — Grok, last

Start with an explicit CLI polling importer, disabled by default. It watches only the configured directory; no whole-disk discovery or new filesystem daemon/dependency. Reuse the import API, supported-media limits, durable receipts, and ambiguous-write reconciliation. First scan is dry-run/preview; applying requires an explicit flag. Wait for files to become stable before hashing/uploading. Handle partially copied files, duplicate bytes, renames, interruption/restart, permission errors, and API unavailability without duplicate writes.

Scope checkpoints to the canonical library target and account identity without storing credentials. Exclude symlinks, hidden/package content, and the checkpoint/output files themselves. Allow explicit collection/shared metadata assignment. Never delete or move source files. Test generated fixtures, account/library changes, and crashes between upload receipt and checkpoint write. Codex performs the final local integration check.

## Deferred

Actual remote offsite replication, S3 backups, online restore, semantic search, per-collection ACLs, and broader redesigns require separate scope. They are not implied by these tasks.
