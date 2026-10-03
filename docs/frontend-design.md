# Kura V3 frontend design — Library workspace

Status: the Library workspace, grid virtualization, cursor pagination, Inspector detail loading, tag edits, favorites, scores, and ordered collections are built in `web/` and Go.

This package answers `TODO.md`. It borrows the *interaction model* of Lightroom Classic's Library module (Grid, Loupe, Compare, Survey, Filmstrip, Library Filter, panels at the edges) and none of its gray skin or photo-editing controls. Everything here fits the existing Elm `web/` app, the Go HTTP API, and PostgreSQL. Items that need Go/API work are marked **[API]**.

Examples use the seeded demo rows (`make db-seed`, query `demo`), for instance `#2006 ruin-explorers.jpeg 1199×836`, `#2004 kson.jpeg 679×437`, `#2009 supernal.jpeg 381×680`. Rows `#1001–#1003` point at files that do not exist and exercise the missing-media state.

---

## 1. Workspace layout

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ TopBar  Kura │ [ KuraQL query ............................ ] 15 │ G E C N │ I │
├────────────┬───────────────────────────────────────────────┬───────────────┤
│ Navigator  │ FilterBar  demo × │ -kson × │        sort · size ▭───  │ Inspector     │
│            ├───────────────────────────────────────────────┤               │
│ Saved      │                                               │ #2006         │
│ searches   │                  Stage                        │ 1199 × 836    │
│            │   Grid | Loupe | Compare | Survey             │ image/jpeg    │
│ Recent     │                                               │ Source …      │
│            │                                               │ Tags …        │
│ Collections├───────────────────────────────────────────────┤ History …     │
│            │ Filmstrip (Loupe / Compare / Survey only)      │               │
├────────────┴───────────────────────────────────────────────┴───────────────┤
│ StatusBar  active #2006 · 3 selected · 15 loaded                           │
└────────────────────────────────────────────────────────────────────────────┘
```

| Region | Size | Purpose |
| --- | --- | --- |
| `TopBar` | 44px tall | Wordmark, KuraQL field (flexible, monospace), result count, mode switch (Grid/Loupe/Compare/Survey with key hints), inspector toggle. |
| `Navigator` | 232px | Saved searches (Lightroom Folders), recent queries, collections/pools. |
| `FilterBar` | 36px, toggle with `\` | The current query as removable term chips, sort, thumbnail size slider, cell-extras toggle. |
| `Stage` | remaining space | The media workspace. Exactly one mode is visible. It is its own scroll container (`#media-grid-viewport`); the page itself never scrolls. |
| `Filmstrip` | 84px | The current result sequence as a horizontal strip, shown in Loupe, Compare, and Survey. Hidden in Grid because the grid already is the sequence. |
| `Inspector` | 320px | Active post identity, dimensions, source, tags, history; selection summary and actions when more than one post is selected. |
| `StatusBar` | 24px | Active id, selection count, loaded/total counts, loading state, transient hints ("Select 2–6 images to survey"). |

### Breakpoints (viewport width)

| Width | Navigator | Inspector | Stage behavior |
| --- | --- | --- | --- |
| ≥ 1440px | Docked | Docked | Full layout. |
| 1100–1439px | Drawer over Stage, closed by default (`Shift+N` / button) | Docked | Grid reflows to the reclaimed width. |
| 760–1099px | Drawer | Drawer from the right over Stage, closed by default; `I` toggles. Opening it never reflows the grid. | Filmstrip stays. |
| < 760px | Drawer (full height) | Bottom sheet, 55vh, `I` toggles | TopBar wraps the query field to a second row. Filmstrip hidden; Loupe shows previous/next buttons. Compare stacks vertically. Survey uses two columns. |

When the docked Inspector's own width falls below 300px (CSS container query on the inspector), it switches to a single-column layout: labels above values, tags wrap, history collapses to the last three entries.

Drawers are non-modal: no backdrop, no focus trap; `Esc` closes an open drawer before it does anything else. Whether each docked panel is shown persists in `localStorage`; drawers start closed.

---

## 2. Visual direction

Restrained and collection-first: the chrome should disappear and the thumbnails carry the color. Continuity with the current palette in `web/index.html`, with a darker stage only where images are inspected at size.

**Color tokens (CSS custom properties)**

| Token | Value | Use |
| --- | --- | --- |
| `--paper` | `#f5f4f1` | App background, TopBar, panels |
| `--paper-raised` | `#fbfaf8` | Inspector sections, drawers, menus |
| `--stage-grid` | `#ebe9e4` | Grid stage background |
| `--cell` | `#e0ddd6` | Cell letterbox / placeholder |
| `--stage-view` | `#1c1b1f` | Loupe/Compare/Survey stage (neutral dark for image fidelity) |
| `--ink` | `#25242a` | Primary text, active outline on light stage |
| `--ink-muted` | `#6b6870` | Secondary text, labels |
| `--rule` | `#dcdad3` | 1px borders between regions |
| `--accent` | `#5e77a8` | Selection, primary action, focus ring |
| `--accent-tint` | `#dce5f6` | Selected cell wash |
| `--accent-on-dark` | `#9fb3dc` | Focus/selection on the dark stage |
| `--danger` | `#9a3f36` | Errors only |

**Type.** `system-ui` stack, no web fonts. Sizes 12 (meta, status), 13 (panels, chips), 14 (controls), 16 (inspector heading). `font-variant-numeric: tabular-nums` for ids, counts, and dimensions. `ui-monospace` for the KuraQL field and hashes.

**Spacing.** 4px base: 4, 8, 12, 16, 24. Panels use 12px padding; grid gap is 4px at sizes ≤ 144px and 8px above.

**Borders and shape.** Regions separated by 1px `--rule`, never by shadows. Cells have a 2px radius, controls 4px. Shadows only on drawers and menus (`0 8px 24px rgb(0 0 0 / .18)`).

**Cell states** (combinable; checked in this order):

| State | Treatment |
| --- | --- |
| Rest | Image `object-fit: contain` centered in a square cell over `--cell`. No footer. |
| Hover | Selection checkbox fades in (top-left, 20px); compact badge (id · dims) appears bottom-left. |
| Selected | 2px `--accent` inset border, `--accent-tint` letterbox wash, filled checkbox always visible. |
| Active | 2px `--ink` outline with 2px offset (outside the cell, so it coexists with the selected border). On the dark stage, `--accent-on-dark`. |
| Keyboard focus | Grid focus follows the active cell (roving focus), so focus-visible equals Active plus a 1px inner white ring. Other controls: 2px `--accent` outline, 2px offset. Focus is never hidden. |
| Missing media | `--cell` with the post id and "media unavailable" in `--ink-muted`; cell keeps its size. |
| Stale (search in flight) | Grid at 55% opacity, 2px indeterminate bar under TopBar. |

**Density.** Thumbnail size is a continuous value, 80–320px in 8px steps (slider in FilterBar, `-` / `=`). Presets: 96, 144 (default), 200, 280. Cells are uniform squares, not justified rows: uniform geometry keeps virtualization, arrow-key row math, and scroll restoration exact for 20,000 items. Cell extras cycle with `J`: none → compact badge on hover (default) → always-on footer (id, dims, media type).

**Overlays.** Loupe, Compare, and Survey are Stage modes, not modal dialogs; TopBar, panels, and Filmstrip stay usable. Only true menus (e.g. "Add to collection") float, anchored to their trigger.

---

## 3. State model

```elm
type alias PostId = String

type Mode
    = Grid
    | Loupe
    | Compare { select : PostId, candidate : PostId }
    | Survey (List PostId) -- 2..6 ids, sequence order

type alias Selection =
    { active : Maybe PostId   -- the current image; drives Inspector and Loupe
    , selected : Set PostId   -- multi-selection; independent of active
    , anchor : Maybe PostId   -- origin for Shift range selection
    }

type alias Sequence =
    { posts : Array PostSummary
    , indexById : Dict PostId Int
    , nextCursor : Maybe String
    , exhausted : Bool
    }

type SearchState
    = Idle
    | Searching { stale : Bool } -- previous Sequence stays visible
    | Ready
    | Failed String

type alias GridViewport =
    { scrollTop : Float, width : Float, height : Float }

type alias ReturnPoint =
    { scrollTop : Float, activeAtEntry : Maybe PostId }

type PanelState = Docked | DrawerOpen | DrawerClosed

type alias Model =
    { route : Route              -- ?q=…, plus &post=…&view=loupe while Loupe is open
    , draftQuery : String
    , search : SearchState
    , sequence : Sequence
    , selection : Selection
    , mode : Mode
    , viewport : GridViewport
    , returnPoint : Maybe ReturnPoint
    , navigator : PanelState
    , inspector : PanelState
    , filterBar : Bool
    , prefs : Prefs              -- thumbSize, cellExtras, panel prefs
    , details : Dict PostId (Remote PostDetail) -- [API]
    , missingMedia : Set PostId
    , hint : Maybe String
    }
```

**Invariants**

- `active`, every id in `selected`, `anchor`, and every id in `Mode` exist in `sequence.indexById`. Any transition that replaces the sequence prunes them.
- Changing `selected` never changes `active`, except when there is no active post yet (the first selected post becomes active).
- Actions target `selected` when non-empty, otherwise `active`. The action bar and Inspector always say which ("Tag 3 selected" vs "Tag #2006").
- `returnPoint` is `Just` exactly when `mode /= Grid`.

**Transitions**

| Event | Effect |
| --- | --- |
| Grid → Loupe/Compare/Survey | Save `ReturnPoint { scrollTop = viewport.scrollTop, activeAtEntry = active }`. The grid stays mounted underneath (`visibility: hidden; inert`, never `display: none`, which would reset its scroll offset). |
| Navigate inside Loupe/Compare/Survey | Changes `active` (and the compare candidate); `selected` is untouched. URL updated with `Nav.replaceUrl` (no history spam). |
| `Esc` / `G` back to Grid | Grid made visible. If `active == activeAtEntry` or the active cell lies inside the saved viewport, `setViewportOf "media-grid-viewport" 0 scrollTop` restores the exact offset. Otherwise scroll the minimum distance to reveal the active cell. Then `Browser.Dom.focus` the active cell. Selection is unchanged. Clear `returnPoint`. |
| New query submitted | `search = Searching { stale = True }`; old sequence stays rendered and dimmed. On success: replace sequence, prune selection/active to ids still present, keep mode if its posts survive (else Grid), keep panels and thumb size. If active survives, reveal it; otherwise scroll to top. URL `q` via `Nav.pushUrl` (back/forward re-runs queries, as today). |
| Search fails | Keep stale sequence visible, show error with Retry in StatusBar. |
| Scroll near end of grid | request the next cursor page, append deduplicated posts to `Array`, and keep the active selection, mode, and scroll position. |
| Resize / panel toggle | Re-measure the grid with `Browser.Dom.getViewportOf`; columns recomputed; the first visible row's top item is kept in view. |
| Reload with `?q=demo&post=2006&view=loupe` | Search, then set active = 2006, enter Loupe with a `ReturnPoint` whose `scrollTop` reveals 2006. |

**Grid geometry** (pure, in `Feature.MediaGrid.Layout`):
`columns = max 1 (floor ((width + gap) / (thumb + gap)))`, `rowHeight = thumb + gap`, `rowOf i = i // columns`.
Rendered rows = visible rows ± 3 overscan; a spacer sets the full height. At 20,000 posts and 144px cells in an 864px stage (5 columns), that is 4,000 rows ≈ 592,000px of scroll height and about 45 rendered cells. Cells are `Html.Keyed` by post id and wrapped in `Html.Lazy`; `scroll` events update the model only when the first visible row changes (the raw `scrollTop` is still stored for restoration). Images use `loading="lazy"` and `decoding="async"`.

---

## 4. Elm module map

Fits the existing `App`, `Domain`, `Api`, `Page`, `Feature`, `Ui` folders. Plain CSS moves from the inline `<style>` to `web/kura.css` (tokens first, then one section per region); no framework.

| Module | Status | Responsibility |
| --- | --- | --- |
| `Main` | change | Flags (`apiBase`, stored prefs), URL wiring, delegates to `Page.Library`. |
| `App.Route` | new | Parse/build `?q=&post=&view=`. Replaces `queryFromUrl` / `queryHref` in `Main`. |
| `App.Keyboard` | new | Decode `keydown` into a `Command` with modifiers; reports whether the target is an editable field. Replaces the `inputFocused` flag. |
| `App.Prefs` | new | `Prefs` type, JSON codec, outgoing port `savePrefs` (single port; read back via flags from `localStorage`). |
| `Domain.Post` | keep | `PostSummary`, `PostDetail`, tag revisions, and optimistic tag-edit results. |
| `Domain.Sequence` | new | Array + index lookup, next/prev/clamp, window around an index, range between two ids, append page. |
| `Domain.Selection` | new | `Selection` and its operations: `click`, `toggle`, `range`, `addRange`, `selectAll`, `clear`, `prune`, `targets`. Pure. |
| `Domain.Query` | new | Query text helpers: split into chips, `addTerm`, `excludeTerm` (`-tag`), `removeTerm`. Uses the PostgreSQL websearch syntax the API already accepts until the KuraQL parser lands. |
| `Api.Post` | change | `search` accepts an optional cursor and limit; `detail id` loads Inspector metadata; `editTags` submits optimistic bulk tag edits. |
| `Page.Library` | new | Model/Msg/update/view for the workspace: regions, mode, return point, panel states, keyboard dispatch. |
| `Feature.MediaGrid` | rewrite | Virtualized uniform grid; `Config msg` record (sequence, selection, thumb, extras, viewport, `onCell : PostId -> Modifiers -> msg`, `onOpen`, `onScroll`, `onMediaError`). |
| `Feature.MediaGrid.Layout` | new | Pure geometry: columns, visible row range, offset of an index, scroll-to-reveal. |
| `Feature.QuickLook` | rewrite | Loupe as a Stage mode: fit / 1:1 (`Z`), position "7 / 15", open original. |
| `Feature.Compare` | new | Select vs candidate, side by side, metadata diff row under each image. |
| `Feature.Survey` | new | 2–6 tiles fitted to the stage, remove-from-survey control per tile. |
| `Feature.Filmstrip` | new | Horizontal window of ±30 items around active; same cell states as the grid. |
| `Feature.Inspector` | new | Identity, dimensions, media type, source, tags, history; selection summary when `selected` > 1. |
| `Feature.QueryEditor` | new | TopBar KuraQL field + FilterBar chips. |
| `Feature.Selection` | change | Action bar: count, clear, tag, add to collection, favorite; tag, favorite, and score editing are live, while collection actions remain disabled with a reason. |
| `Feature.Navigator` | new | Saved searches, recent queries, collection creation, membership, and compact ordering controls. |
| `Feature.TagEditor` | integrated | Compact Inspector add/remove editor for active or selected posts. |
| `Feature.RevisionDiff` | later | Inspector history entries are currently rendered as a compact list; richer diffs remain open. |
| `Ui.Layout` | new | Region shells, drawer/sheet wrappers, breakpoints. |
| `Ui.Button`, `Ui.Kbd`, `Ui.Chip`, `Ui.Icon` | new | Small view helpers; icons are inline SVG paths, no icon library. |

---

## 5. Keyboard and pointer behavior

### Keyboard

Global commands are ignored while focus is in an editable field, except `Esc`. Space, arrows, Home/End, PageUp/PageDown, `Ctrl/Cmd+A`, and `Ctrl/Cmd+D` are captured with `Html.Events.preventDefaultOn "keydown"` on the shell root (a `Browser.Events` subscription cannot prevent page scroll or browser shortcuts).

| Key | Grid | Loupe | Compare | Survey |
| --- | --- | --- | --- | --- |
| `←` `→` | Active to previous/next item | Previous/next in sequence (clamped, no wrap; loads next page at the end) | Previous/next candidate | Move active between tiles |
| `↑` `↓` | Active up/down one row | — | `↑` candidate becomes select; `↓` swap sides | Move active between tile rows |
| `Shift` + arrows | Move active and set selection to range anchor…active | — | — | — |
| `Alt` + `←` `→` | — | Step only through selected items | Step candidate through selected items | — |
| `Home` / `End` | First / last loaded item | Same | — | — |
| `PageUp` / `PageDown` | Move one viewport of rows | — | — | — |
| `Space` / `E` | Open Loupe on active | Back to Grid (`Space`) | Loupe on active | Loupe on active |
| `Enter`, double-click | Open Loupe on active | — | — | — |
| `C` | Compare (rules below) | Compare | — | Compare |
| `N` | Survey (rules below) | Survey | Survey | — |
| `G`, `Esc` | — | Grid with scroll and focus restored | Same | Same |
| `S` | Toggle active in selection | Same | Same | Same (removes tile when it leaves) |
| `Ctrl/Cmd+A` | Select all loaded | Same | — | — |
| `Ctrl/Cmd+D` | Clear selection | Same | Same | Same → Grid |
| `-` / `=` | Smaller / larger thumbnails | — | — | — |
| `Z` | — | Toggle fit / 1:1 | Toggle fit / 1:1 on both | — |
| `J` | Cycle cell extras | — | — | — |
| `/` | Focus query field | Same | Same | Same |
| `\` | Toggle FilterBar | Same | Same | Same |
| `I` | Toggle Inspector | Same | Same | Same |
| `Shift+N` | Toggle Navigator | Same | Same | Same |
| `T` | Focus Inspector tag field **[API]** | Same | Same | Same |
| `B` | Add targets to the target collection **[API]** | Same | Same | Same |
| `F` | Toggle favorite on targets | Same | Same | Same |
| `?` | Shortcut sheet | Same | Same | Same |

`Esc` priority: 1) in a text field, revert the draft and blur; 2) close an open menu or drawer; 3) leave Loupe/Compare/Survey for Grid; 4) in Grid, nothing. `Esc` never clears the selection.

**Compare rules.** 0–1 selected: select = active, candidate = next item in the sequence. Exactly 2 selected: those two, active on the left. More than 2: select = active (or first selected), `←` `→` cycle the candidate through the other selected items.
**Survey rules.** Fewer than 2 selected: stay put, StatusBar hint "Select 2–6 images to survey". 2–6: all of them. More than 6: the first 6 in sequence order starting at active, with the hint "Showing 6 of 9 selected".

**Focus order.** TopBar → Navigator → FilterBar → Stage (one tab stop; `role="listbox"` with `aria-multiselectable`, roving `tabindex`, `aria-selected` per cell, active cell has `tabindex="0"`) → Filmstrip (one tab stop) → Inspector → StatusBar. Changing active by keyboard moves DOM focus with it via `Browser.Dom.focus`.

### Pointer

| Gesture | Effect |
| --- | --- |
| Click cell | active = it, selected = {it}, anchor = it |
| `Ctrl/Cmd`+click or click the cell checkbox | Toggle it in selected; anchor = it; active unchanged |
| `Shift`+click | selected = range(anchor, it); active unchanged |
| `Ctrl/Cmd+Shift`+click | Add range(anchor, it) to selected |
| Double-click | Loupe on it |
| Click empty grid space | Nothing (selection is never cleared by accident) |
| Click Filmstrip item | active = it (Loupe shows it); modifiers behave as in the grid |
| Click Loupe image | Toggle fit / 1:1 |
| Click Inspector tag | Append the tag to the query; `Alt`+click appends `-tag` |
| Click FilterBar chip × | Remove that term and re-run the search |
| Survey tile × | Remove from survey (deselect only; never deletes the post) |
| Touch | Checkboxes always visible once anything is selected; long-press = `Ctrl`+click |

Marquee drag-selection and panel resizing are deferred.

---

## 6. Implementation sequence

| # | Step | Kind | State |
| --- | --- | --- | --- |
| 1 | Move CSS to `web/kura.css` with tokens; build the region shell (TopBar, Navigator, Stage, Inspector, StatusBar) and breakpoints with placeholder panels. | Visual, Elm only | Done |
| 2 | `Domain.Sequence`, `Domain.Selection`, `App.Keyboard`, `App.Route`; replace `List` + `Maybe Int` with `Array` + active/selected/anchor; roving focus. | Elm only | Done |
| 3 | Uniform-cell grid with density slider, cell extras, missing-media state, prefs port. | Visual, Elm only | Done, virtualized |
| 4 | Loupe as a Stage mode, Filmstrip, return point and scroll/focus restoration; URL `post`/`view`. | Elm only | Done |
| 5 | Compare and Survey. | Elm only | Done |
| 6 | Inspector using `PostSummary` fields (id, dims, media type, URLs; tags when present); FilterBar chips; keep stale results while searching; prune selection across re-queries. | Elm only | Done |
| 7 | Cursor pagination: `GET /api/posts?q=&cursor=&limit=` returning `next_cursor` (envelope change only, `PostSummary` unchanged); then incremental loading near the end of the grid. The grid is already virtualized. | **[API]** + Elm | Done |
| 8 | Return `tags` in `PostSummary` (Elm already decodes it optionally); `GET /api/posts/{id}` detail with source, artist, hash, file size, created time, tags, revisions. Needs tag/source/hash schema. | **[API]** + Elm | Done |
| 9 | Empty query browses newest posts through the same cursor API; Navigator "All posts". | **[API]** | Done |
| 10 | Revision-backed mutations: tag edits, collections/pools, favorite/score; wire Selection actions, `T`, `B`, `F`. Saved searches start in `localStorage`, move server-side later. | **[API]** + Elm | Tag/favorite/score/collections done |

The completed Library slices can be verified with the 15 seeded `demo` rows. Tag and reaction revision history, collection membership/order, plus the `T`, `B`, and `F` mutations are live; richer revision diffs and saved-search server state remain open.

---

## 7. Acceptance checklist (browser, `?q=demo`, 1440px wide unless stated)

1. 15 cells render; `#1001–#1003` show the missing-media placeholder at full cell size; the page body never scrolls, only the Stage.
2. `-` / `=` and the slider change cell size between 80 and 320px; at 96px with both panels docked the grid shows at least 8 columns; `J` cycles cell extras.
3. Clicking `#2006` makes it active and selected; the Inspector shows `2006`, `1199 × 836`, `image/jpeg`.
4. `Ctrl`+click `#2004` and `#2009`: 3 selected, `#2006` still active, Inspector still shows `#2006`. `Shift`+click selects a contiguous range from the anchor.
5. With the Stage scrolled (thumb size 280), `Space` opens Loupe on the active post with the Filmstrip; `→` / `←` step through the sequence and stop at the ends; `Esc` returns to Grid with `document.getElementById("media-grid-viewport").scrollTop` unchanged, the same selection, and keyboard focus on the active cell.
6. Selecting exactly `#2004` and `#2006` and pressing `C` shows both side by side with dimensions; `↓` swaps them; `Esc` returns to Grid as in item 5.
7. With 4 selected, `N` shows 4 tiles; with 1 selected, `N` stays in Grid and shows the survey hint.
8. Changing the query to `demo -kson`: the grid keeps its panels and thumb size, dims while loading, then drops `#2004`; the other selected posts remain selected and `#2006` stays active.
9. Tab moves TopBar → Navigator → FilterBar → Grid (one stop) → Inspector, with a visible focus ring on each; arrow keys move the focus ring inside the grid.
10. At 1000px the Inspector becomes a drawer toggled by `I` without reflowing the grid; at 600px it is a bottom sheet; no horizontal scrollbar at any width ≥ 320px.
11. Loading `?q=demo&post=2006&view=loupe` opens Loupe on `#2006`; `Esc` lands in Grid with `#2006` visible and focused.
