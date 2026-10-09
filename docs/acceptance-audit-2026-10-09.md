# Acceptance audit — 2026-10-09 (forced live pass)

Scope: [docs/frontend-design.md](frontend-design.md) acceptance checklist plus the
live product slices that landed since (collections, uploads, authentication,
moderation, job polling, derivative status). One forced pass: OrbStack,
PostgreSQL, the Go API, the Elm frontend, and headless Chromium were all
brought up for verification, then stopped. Nothing was pushed.

## Environment

- OrbStack revived via `orbctl start`; Postgres `kura-v3-postgres` on host port
  55432 (`kura/kura`, db `kura_v3_dev`), 17 posts, all 11 migrations applied.
- API: `go -C server run` on `:8080` with the container credentials
  (`GOCACHE=/tmp/gocache`, `--noproxy '*'`, escalated sandbox for loopback).
- Web: `python3 -m http.server 8000 --directory web` with a freshly built
  (git-ignored) `web/dist/elm.js`.
- Browser: Playwright headless Chromium shell, 1440px unless stated.

## Automated evidence

- `go -C server vet ./...`: clean.
- `go -C server test`: pass in `auth`, `posts`, `httpapi`, `jobs`, `media`.
- `elm make src/Main.elm`: success.
- `go -C cli test`: fails only because the default sandbox denies loopback
  listeners (`httptest.NewServer` panics); environmental, not applicational.

## HTTP evidence (live, dev database)

- `GET /api/posts?q=demo&limit=60`: 200, 15 posts, ~2ms.
- Cursor pagination chains: page 1 `2012–2008`, page 2 `2007–2003`.
- `demo -kson`: 14 posts, #2004 absent. Empty query browses newest first.
- `GET /api/posts/2006`: `1199×836 image/jpeg` with source/artist/hash.
- `GET /api/saved-searches`: `[]`. Collections list 3 rows; collection 1
  serves #2004 + #2006 with dimensions.
- Moderation queue `state=pending`: `[]` (200); missing state rejected.
- `/media/demo/kson.jpeg`: 200, `image/jpeg`, ~50KB.
- Invalid upload rejected 400 with no DB write. A mutation with a bad body
  returns 400, not 401, proving open local-dev mode (`KURA_API_TOKEN` unset).

## Browser evidence (headless Chromium, screenshots pixel-inspected)

- 15 cells; #1001–#1003 missing-media placeholders; page body never scrolls.
- Click selects; Inspector shows `2006`, `1199 × 836`, `image/jpeg`.
- Checkbox multi-select keeps active; Cmd+click toggles (3 selected, active kept).
- Loupe steps 7/15 → 8/15 with filmstrip; Esc restores scrollTop exactly
  (300 → 300) with focus on the active cell.
- Compare renders two panes with metadata diff rows; Survey shows 4 tiles with
  per-tile remove, and the 1-selected survey hint.
- `?q=demo&post=2006&view=loupe` deep-links; Esc lands focused on `cell-2006`.
- Navigator shows saved searches, collections, and ordering controls.
- 96px thumbs give 8 columns; `J` cycles extras; arrows move grid focus;
  no horizontal scroll at 600px.

## Unavailable / not exercised

- Job polling end-to-end: `GET /api/jobs/{id}` returns 503 `jobs unavailable`
  and the jobs table is empty. The `jobs` package unit tests pass, so polling
  and derivative status are code-complete but unwired in this server.
- Valid-upload persistence and tag/favorite/score mutations were deliberately
  not fired (dev-DB writes avoided); only their validation paths were hit.

## Platform finding

- Ctrl+click never reaches the app on macOS Chromium: the OS turns it into a
  right-click (`contextmenu`, no `click` event). Cmd+click is the working
  toggle path there. The "Ctrl/Cmd+click" contract holds on other platforms.
