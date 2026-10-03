# Kura V3 frontend design handoff — Opus 5.5

Status: design package delivered in [`docs/frontend-design.md`](docs/frontend-design.md). Library browsing, cursor pagination, Inspector details, newest browsing, revision-backed tag edits, favorites, scores, ordered collections, and collection browsing are implemented. Richer revision diffs remain in the implementation sequence.

## Goal

Design Kura's frontend around the interaction model of **Lightroom Classic Library**, especially the feeling of manipulating a large image collection in place. Do not copy Lightroom's gray visual styling or reproduce its photo-editing controls.

The current implementation is Elm in `web/`. Keep Elm as the frontend runtime. Do not introduce React, TypeScript, Next.js, a CSS framework, a component library, or a new frontend architecture.

## Product model to design

Kura maps the Library workflow like this:

| Lightroom | Kura |
| --- | --- |
| Folders | Saved searches |
| Keywords | Booru tags |
| Rating | Score / favorite |
| Collections | Collections / pools |
| Metadata | Source, artist, dimensions, hash |
| Compare | Duplicate/version comparison |
| Filmstrip | Current search-result sequence |
| Loupe | Quick Look |
| Library Filter | KuraQL |

The center should be the media workspace. Navigation and filtering belong at the edges. Metadata and actions belong in an inspector.

## Required interaction decisions

Design concrete states and transitions for:

- Dense MediaGrid with adjustable thumbnail size/density and little wasted space.
- Separate active image and multi-selection state. Selecting several images must not lose the current image.
- Quick Look/Loupe opened with `Space`, with previous/next keyboard navigation.
- `Esc` returning to the exact prior grid scroll position and selection.
- Inspector showing post identity, dimensions, source, tags, and history beside the workspace.
- Filtering the same grid by Kura query/tag metadata without losing the workspace.
- Compare for 2 images and Survey for 2–6 images.
- Selection actions such as tags, collections, rating, and future bulk edits.
- A result sequence/filmstrip relationship between Grid, Quick Look, Compare, and Survey.
- Keyboard navigation and visible focus states.
- Responsive behavior when the inspector is narrow or hidden.

## Deliverable

Return a concise design package for the implementation agents:

1. A page/workspace layout with named regions and responsive breakpoints.
2. A visual direction with color, type, spacing, borders, focus, selection, density, and overlay rules. Keep the design restrained and collection-focused; old Lightroom is an interaction reference, not a skin.
3. A state model for grid, active item, selection, Quick Look, Compare, Survey, inspector visibility, filters, and scroll restoration.
4. An Elm-oriented component/module map that fits the existing `Domain`, `Api`, `Page`, `Feature`, and `Ui` structure.
5. Concrete behavior for keyboard commands and pointer interactions.
6. A short implementation sequence, identifying which work is visual and which work requires Go/API changes.
7. A small acceptance checklist that can be verified in the browser.

Use the existing local demo media under `web/static/media/demo` when showing examples. The supplied Safebooru digit GIFs and header image are references for a later branded pass; do not turn them into a decorative header or let them dictate the Library workspace layout.

## Constraints

- Preserve the existing Elm/Go/PostgreSQL architecture and HTTP API.
- Do not change backend contracts in this design handoff.
- Do not add realtime transport, Redis, object storage, or speculative services.
- Do not implement design assets, upload workflows, or authentication here.
- Keep the design useful for browsing 20,000 images.
- The next agent will implement the approved design in Elm and integrate any required API changes.

## Handoff protocol

When the design package is complete, commit and push it to `main`. Stop after publishing and wait for the user to signal that implementation should begin. Do not start the Elm implementation from this handoff.
