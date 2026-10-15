"""Model the library grid's page and DOM window at 100k posts.

This does not start a browser and does not measure interaction delay.
SQL timings from library-perf-100k.py are not browser timings. The
checks at the bottom of the report stay unverified until they are run
in a browser against a disposable server.
"""

from __future__ import annotations

import json

POSTS = 100_000
SEARCH_FETCH = 61
SIMILAR_FETCH = 31
PAGE = 60
OVERSCAN_ROWS = 3
THUMB = 180


def geometry(width: float, thumb: int = THUMB) -> dict:
    gap = 8 if thumb <= 144 else 16
    padding = 16
    inner = max(0, width - 2 * padding)
    columns = max(1, int((inner + gap) // (thumb + gap)))
    cell = max(1, (inner - gap * (columns - 1)) / columns)
    return {
        "columns": columns,
        "cell": cell,
        "gap": gap,
        "row_height": cell + gap,
        "padding": padding,
    }


def visible_range(g: dict, scroll_top: float, height: float, count: int) -> tuple[int, int]:
    rows = (count + g["columns"] - 1) // g["columns"]
    if rows <= 0:
        return (0, 0)
    first_row = max(0, int((scroll_top - g["padding"]) // g["row_height"]) - OVERSCAN_ROWS)
    last_row = min(rows - 1, int((scroll_top + height - g["padding"]) // g["row_height"]) + OVERSCAN_ROWS)
    return (first_row * g["columns"], min(count, (last_row + 1) * g["columns"]))


def pages_fetched(loaded: int, fetch: int) -> int:
    if loaded <= 0:
        return 0
    full = loaded // (fetch - 1)
    return full if loaded % (fetch - 1) == 0 else full + 1


def scenario(name: str, width: float, height: float, scroll_top: float, loaded: int, fetch: int) -> dict:
    g = geometry(width)
    start, end = visible_range(g, scroll_top, height, loaded)
    return {
        "name": name,
        "viewport": {"width": width, "height": height, "scroll_top": scroll_top},
        "loaded_posts": loaded,
        "requests": pages_fetched(loaded, fetch),
        "visible_dom_cells": end - start,
        "visible_from": start,
        "visible_to": end,
        "columns": g["columns"],
    }


def report() -> dict:
    desktop = scenario("first_load", 1280, 800, 0, PAGE, SEARCH_FETCH)
    deep = scenario("deep_scroll", 1280, 800, 400_000, 100_000, SEARCH_FETCH)
    narrow = scenario("narrow_first_load", 390, 700, 0, PAGE, SEARCH_FETCH)
    similar = scenario("similarity_page", 1280, 800, 0, 30, SIMILAR_FETCH)
    return {
        "corpus_posts": POSTS,
        "image_bytes": 0,
        "search_fetch": SEARCH_FETCH,
        "similar_fetch": SIMILAR_FETCH,
        "browser_measured": False,
        "interaction_delay_ms": None,
        "model": [desktop, deep, narrow, similar],
        "retention": {
            "dom": "MediaGrid renders visibleRange plus 3 overscan rows, not the loaded sequence.",
            "memory": "Domain.Sequence.append keeps every fetched page until the query changes.",
        },
        "unverified_browser_checks": [
            "First load of a disposable 100k library paints only the visible window.",
            "Repeated pagination does not grow the visible DOM with the loaded sequence.",
            "Deep scrolling keeps a bounded cell count and does not mount 100k images.",
            "Selection, Compare, and return restore the previous scroll window.",
            "A query change drops the previous sequence instead of appending to it.",
            "Reopening the inspector does not refetch the whole loaded sequence.",
            "Interaction delay is measured in the browser, not copied from SQL.",
        ],
    }


def main() -> None:
    print(json.dumps(report(), indent=2))


if __name__ == "__main__":
    main()
