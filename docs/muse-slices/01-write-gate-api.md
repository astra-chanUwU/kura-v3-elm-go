# Muse Spark 1.2 high — Write-gate discovery and validation API

Work in /Users/astrochan/Documents/Workstation/kura-v3-elm-go. Implement only this bounded slice under supervising Codex review. Release criterion: "I can safely manage my actual image library here every day." Read docs/daily-library-plan.md and the relevant current code before editing. Inspect git status/diff first and preserve unrelated edits. Keep Elm/Go/PostgreSQL and existing local storage; no new frameworks or services. If a prerequisite is absent, report it and stop instead of expanding the scope. Never read or expose credentials, private media, or ignored import manifests. Never truncate/reset the development database or reseed real data. Database tests may truncate tables: run them only against a disposable test database, and serially across packages. Do not run another agent or edit concurrently with another writer.

Prerequisites: none. Own server/internal/httpapi access handler/router/CORS tests only.

Add GET /api/capabilities using the existing deployment token comparison/actor contract:
- Open local mode, no bearer: 200 {"writes_require_auth":false,"can_write":true,"actor":"local"}.
- Gated mode, no bearer: 200 {"writes_require_auth":true,"can_write":false,"actor":null}.
- Gated mode, valid bearer: 200 {"writes_require_auth":true,"can_write":true,"actor":"system"}.
- Gated mode, supplied invalid/malformed bearer: 401 {"error":"write capability required"}.
No response contains a secret or token digest. This is non-mutating validation, not a login/session/password rollout. An Authorization header that is supplied but malformed must not be mistaken for an absent credential in gated mode. Fix local CORS allowed methods to include PATCH while preserving existing allowed origins/headers and public reads. Keep all existing mutation gates and CLI behavior unchanged.

Test exact open/gated/valid/missing/invalid/malformed responses; public search/media behavior; saved-search read ownership; protected mutation behavior; PATCH preflight with Authorization. Do not touch Elm, database schema, import scripts, or collection behavior.

Verify with focused meaningful tests, make check, and git diff --check. Use make web-test for the existing Elm harness. If database/browser evidence is unavailable, say exactly what was not run; do not claim it passed. Report changed files, commands/results, behavior, and remaining limitations. Do not commit or push: stop at a reviewable diff for supervising Codex. Do not implement subsequent slices. Do not rewrite Page.Library or refactor unrelated code.
