"""Bounds for the 100k browser-scale model. No server and no browser."""

import unittest

import browser_scale_100k as scale


class BrowserScaleTests(unittest.TestCase):
    def test_fixture_has_no_image_bytes_and_does_not_claim_a_browser_timing(self):
        report = scale.report()
        self.assertEqual(report["corpus_posts"], 100_000)
        self.assertEqual(report["image_bytes"], 0)
        self.assertFalse(report["browser_measured"])
        self.assertIsNone(report["interaction_delay_ms"])
        self.assertGreaterEqual(len(report["unverified_browser_checks"]), 7)

    def test_first_viewport_dom_stays_far_below_the_corpus(self):
        report = scale.report()
        first = report["model"][0]
        deep = report["model"][1]
        narrow = report["model"][2]
        self.assertLess(first["visible_dom_cells"], 80)
        self.assertLess(deep["visible_dom_cells"], 80)
        self.assertLess(narrow["visible_dom_cells"], 40)
        self.assertGreater(deep["loaded_posts"], deep["visible_dom_cells"])
        self.assertGreater(deep["requests"], 1)

    def test_page_fetches_match_the_sql_harness_sizes(self):
        self.assertEqual(scale.SEARCH_FETCH, 61)
        self.assertEqual(scale.SIMILAR_FETCH, 31)
        self.assertEqual(scale.pages_fetched(60, 61), 1)
        self.assertEqual(scale.pages_fetched(120, 61), 2)
        self.assertEqual(scale.pages_fetched(30, 31), 1)

    def test_visible_window_tracks_scroll_without_mounting_the_corpus(self):
        g = scale.geometry(1280)
        start, end = scale.visible_range(g, 0, 800, 100_000)
        later, later_end = scale.visible_range(g, 50_000, 800, 100_000)
        self.assertEqual(start, 0)
        self.assertLess(end, 200)
        self.assertGreater(later, start)
        self.assertLess(later_end - later, 200)


if __name__ == "__main__":
    unittest.main()
