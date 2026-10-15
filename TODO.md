# Cursor / Grok handoff: online backup consistency

Use Grok 4.7 with High reasoning if that model is available in Cursor. This task is a bounded investigation and executable proof, before implementing online backup creation. Codex owns integration and final acceptance.

## Start from the published baseline

- Fetch and inspect `origin/main`, the current branch, status, and any `AGENTS.md` instructions. Report the starting commit.
- Use a separate clean checkout or worktree on `codex/online-backup-proof`. Do not reset, clean, stash, or overwrite the coordinator's working directory.
- Read this file, `scripts/library-backup.py`, `server/internal/recovery`, the media store implementations, upload transactions, derivative/fingerprint jobs, HTTP middleware, and their existing tests.
- Preserve `docs/frontend-design.md` and `docs/design-concepts/`. Never read, print, or commit local secrets or actual library images. Use generated files and uniquely named disposable local databases only.

## Determine the smallest correct online-backup contract

The browser currently lists and verifies existing local backups. Creation and restore still use the offline utility. The target is an admin clicking **Back up now** while browsing stays available. Restore remains an explicit offline operation. Private-library account roles and session/origin checks must apply.

Investigate whether an exported PostgreSQL snapshot plus a list of media references read from that same snapshot is sufficient. Prove the actual publication order and immutability of originals and derivatives, including pending uploads, worker completion, Trash/restore, hidden posts, and orphan files. Do not assume a directory walk and an unrelated database dump form a consistent backup. If immutable references cannot be guaranteed, identify the exact writer/worker coordination required and keep any pause bounded.

Specify:

- The snapshot lifetime, dump invocation, referenced-media inventory, manifest hashes, and atomic publication of a completed backup.
- A fixed administrator-configured destination, opaque backup IDs, private permissions, path/symlink checks, cancellation, timeout, disk-full behavior, and one active backup per library across processes.
- Admin-only asynchronous request/status behavior, cookies and origin checks, restart/failure recovery, and what the browser can truthfully call complete.
- Compatibility with the existing verifier and offline restore. Do not claim S3 backup support without a separate consistency proof; a clear local-only restriction is acceptable.
- A concise list of files and interfaces an implementation would change. Avoid a new service, queue framework, or dependency unless evidence requires it.

## Executable evidence

Add only focused tests or a disposable proof harness, preferably under `scripts/` or a new test file. Do not change product behavior yet. Exercise a database snapshot while concurrent generated uploads and derivative jobs finish; verify that every row in the restored snapshot has the expected original/ready-variant bytes and hashes. Also cover interrupted copying, missing/corrupt media, and a failed dump: none may appear as a completed backup. Reuse the existing disposable-database guards and clean up only resources the proof created. Never log connection strings or credentials, and never put passwords in command arguments.

## Stopping point and report

Return one recommended design, evidence for its consistency, failed assumptions, precise test results including skips, and the implementation file/interface list. Do not implement online creation or online restore in this task. Keep the report in your response rather than creating another design document. Commit only the test/proof scope on your isolated branch; report its hash for Codex to review. Do not merge or push to `main`.
