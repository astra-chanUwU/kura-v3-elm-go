"""Guards for the disposable 100k library-performance proof."""

import importlib.util
from pathlib import Path
import unittest


SCRIPT = Path(__file__).with_name("library-perf-100k.py")
SPEC = importlib.util.spec_from_file_location("kura_library_perf_100k", SCRIPT)
PERFORMANCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PERFORMANCE)
ROOT = SCRIPT.parents[1]


class LibraryPerf100kTests(unittest.TestCase):
    def test_local_admin_url_is_moved_to_environment_fields(self):
        environment, database = PERFORMANCE.local_admin_environment(
            "postgresql://perf-user:sensitive-value@127.0.0.1:55432/postgres?sslmode=disable"
        )
        self.assertEqual(database, "postgres")
        self.assertEqual(environment["PGHOST"], "127.0.0.1")
        self.assertEqual(environment["PGPORT"], "55432")
        self.assertEqual(environment["PGUSER"], "perf-user")
        self.assertEqual(environment["PGPASSWORD"], "sensitive-value")
        self.assertNotIn("postgresql://", " ".join(environment.values()))

    def test_url_without_password_is_accepted(self):
        environment, database = PERFORMANCE.local_admin_environment(
            "postgresql://postgres@127.0.0.1:55432/postgres?sslmode=disable"
        )
        self.assertEqual(database, "postgres")
        self.assertEqual(environment["PGPASSWORD"], "")

    def test_nonlocal_database_url_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "local postgresql URL"):
            PERFORMANCE.local_admin_environment("postgresql://u:p@example.invalid/postgres")

    def test_unexpected_connection_parameters_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "local postgresql URL"):
            PERFORMANCE.local_admin_environment("postgresql://u:p@localhost/postgres?options=secret")

    def test_disposable_database_cannot_be_the_admin_database(self):
        with self.assertRaisesRegex(ValueError, "local postgresql URL"):
            PERFORMANCE.local_admin_environment(
                "postgresql://postgres@127.0.0.1/kura_s7perf_0123456789abcdef01234567?sslmode=disable"
            )

    def test_generated_database_name_has_only_owned_prefix_and_random_token(self):
        self.assertEqual(
            PERFORMANCE.generated_database_name("0123456789abcdef01234567"),
            "kura_s7perf_0123456789abcdef01234567",
        )
        with self.assertRaisesRegex(ValueError, "kura_s7perf_"):
            PERFORMANCE.generated_database_name("existing_database")
        with self.assertRaisesRegex(ValueError, "kura_s7perf_"):
            PERFORMANCE.assert_disposable_database("kura_perf_0123456789abcdef01234567")
        with self.assertRaisesRegex(ValueError, "kura_s7perf_"):
            PERFORMANCE.assert_disposable_database("postgres")

    def test_corpus_shape_matches_the_seed_rules(self):
        self.assertTrue(PERFORMANCE.is_published_visible(99999))
        self.assertFalse(PERFORMANCE.is_published_visible(100000))
        self.assertEqual(PERFORMANCE.moderation_state(1), "pending")
        self.assertEqual(PERFORMANCE.moderation_state(2), "hidden")
        self.assertEqual(PERFORMANCE.moderation_state(3), "rejected")
        self.assertEqual(len(PERFORMANCE.descending(PERFORMANCE.is_published_visible)), 95000)
        self.assertEqual(len(PERFORMANCE.descending(PERFORMANCE.is_trashed)), 2000)
        self.assertEqual(len(PERFORMANCE.descending(lambda post_id: PERFORMANCE.moderation_state(post_id) == "pending")), 1000)
        visible = PERFORMANCE.descending(PERFORMANCE.is_published_visible)
        batch = PERFORMANCE.descending(PERFORMANCE.is_batch_seven)
        pending = PERFORMANCE.descending(lambda post_id: PERFORMANCE.moderation_state(post_id) == "pending")
        trashed = PERFORMANCE.descending(PERFORMANCE.is_trashed)
        rare = PERFORMANCE.descending(PERFORMANCE.is_rare_visible)
        self.assertEqual(PERFORMANCE.prefix_end(visible), (10004, 9500))
        self.assertEqual(PERFORMANCE.prefix_end(batch), (10007, 100))
        self.assertEqual(PERFORMANCE.prefix_end(pending), (10001, 100))
        self.assertEqual(PERFORMANCE.prefix_end(trashed), (10050, 200))
        self.assertEqual(len(rare), 95)
        self.assertEqual(rare[59], 36889)
        later_id, distance, remaining = PERFORMANCE.similarity_later_cursor()
        self.assertEqual((later_id, distance, remaining), (17907, 6, 60))
        self.assertEqual(len(PERFORMANCE.near_copies()), 240)
        self.assertTrue(all(PERFORMANCE.is_published_visible(post_id) for post_id, _distance in PERFORMANCE.near_copies()))

    def test_representative_cases_cover_first_and_later_pages(self):
        cases = PERFORMANCE.query_cases()
        self.assertEqual([name for name, *_rest in cases], [
            "search_newest_first",
            "search_newest_later",
            "search_tag_first",
            "search_tag_later",
            "search_text_rare_first",
            "search_text_rare_later",
            "moderation_pending_first",
            "moderation_pending_later",
            "trash_first",
            "trash_later",
            "similarity_first",
            "similarity_later",
        ])
        expected = {name: rows for name, _key, _types, _args, rows in cases}
        self.assertEqual(expected["search_newest_first"], 61)
        self.assertEqual(expected["search_newest_later"], 61)
        self.assertEqual(expected["search_tag_later"], 61)
        self.assertEqual(expected["search_text_rare_first"], 61)
        self.assertEqual(expected["search_text_rare_later"], 35)
        self.assertEqual(expected["moderation_pending_later"], 61)
        self.assertEqual(expected["trash_later"], 61)
        self.assertEqual(expected["similarity_first"], 31)
        self.assertEqual(expected["similarity_later"], 31)
        by_name = {name: execute for name, _key, _types, execute, _rows in cases}
        self.assertIn("NULL", by_name["search_newest_first"])
        self.assertNotIn("10004", by_name["search_newest_first"])
        self.assertIn("10004", by_name["search_newest_later"])
        self.assertIn("ARRAY['batch_7']::text[]", by_name["search_tag_first"])
        self.assertIn("10007", by_name["search_tag_later"])
        self.assertIn("'rareanchor'", by_name["search_text_rare_first"])
        self.assertIn("36889", by_name["search_text_rare_later"])
        self.assertIn("'pending'", by_name["moderation_pending_first"])
        self.assertIn("10001", by_name["moderation_pending_later"])
        self.assertIn("10050", by_name["trash_later"])
        self.assertIn("17907", by_name["similarity_later"])
        self.assertIn("x'0000000000000000'::bit(64)", by_name["similarity_first"])

    def test_seed_is_synthetic_metadata_with_required_states(self):
        sql = PERFORMANCE.seed_sql()
        self.assertIn("generate_series(1, 100000)", sql)
        self.assertIn("dhash-v1", sql)
        self.assertIn("rareanchor", sql)
        self.assertIn("pending", sql)
        self.assertIn("hidden", sql)
        self.assertIn("rejected", sql)
        self.assertIn("deleted_at", sql)
        self.assertIn("/synthetic/media/", sql)
        self.assertNotIn("postgresql://", sql)
        self.assertNotIn("embedding", sql.lower())
        self.assertNotIn("semantic", sql.lower())

    def test_measured_sql_is_the_application_statements(self):
        statements = PERFORMANCE.load_statements()
        self.assertIn("deleted_at IS NULL", statements["search"])
        self.assertIn("moderation_state = 'published'", statements["search"])
        self.assertIn("id < $18::bigint", statements["search"])
        self.assertIn("ORDER BY posts.id DESC", statements["search"])
        self.assertIn("moderation_state = $1::text", statements["moderation"])
        self.assertIn("id < $3::bigint", statements["moderation"])
        self.assertIn("deleted_at IS NOT NULL", statements["trash"])
        self.assertIn("(deleted_at, id) < ($1::timestamptz, $2::bigint)", statements["trash"])
        self.assertIn("algorithm = 'dhash-v1'", statements["similarity"])
        self.assertIn("bit_count(f.fingerprint # $2::bit(64))", statements["similarity"])
        self.assertIn("p.id <> $1::bigint", statements["similarity"])
        self.assertNotIn("embedding", statements["similarity"].lower())

    def test_page_sizes_match_the_library_ui(self):
        search = (ROOT / "web" / "src" / "Feature" / "Search.elm").read_text(encoding="utf-8")
        moderation = (ROOT / "web" / "src" / "Feature" / "Moderation.elm").read_text(encoding="utf-8")
        similarity = (ROOT / "web" / "src" / "Feature" / "Similarity.elm").read_text(encoding="utf-8")
        trash = (ROOT / "web" / "src" / "Feature" / "Trash.elm").read_text(encoding="utf-8")
        distance = (ROOT / "server" / "internal" / "posts" / "similarity.go").read_text(encoding="utf-8")
        self.assertIn("pageSize =\n    60", search)
        self.assertIn("pageLimit =\n    60", moderation)
        self.assertIn("pageLimit =\n    30", similarity)
        self.assertIn("Nothing 60", trash)
        self.assertIn("DefaultSimilarDistance = 8", distance)
        self.assertEqual(PERFORMANCE.SEARCH_FETCH, 61)
        self.assertEqual(PERFORMANCE.SIMILAR_FETCH, 31)
        self.assertEqual(PERFORMANCE.SIMILAR_DISTANCE, 8)

    def test_plan_summary_reports_scans_without_query_text(self):
        report = [{
            "Plan": {
                "Node Type": "Limit",
                "Actual Rows": 31,
                "Shared Hit Blocks": 3,
                "Plans": [
                    {
                        "Node Type": "Sort",
                        "Plans": [
                            {
                                "Node Type": "Seq Scan",
                                "Relation Name": "similarity_fingerprints",
                                "Shared Read Blocks": 10,
                            }
                        ],
                    }
                ],
            },
            "Planning Time": 0.5,
            "Execution Time": 12.25,
        }]
        summary = PERFORMANCE.summarize_explain(report)
        self.assertEqual(summary["rows"], 31)
        self.assertEqual(summary["execution_ms"], 12.25)
        self.assertEqual(summary["seq_scans"], "similarity_fingerprints")
        self.assertEqual(summary["sorts"], "Sort")
        self.assertEqual(summary["shared_hit_blocks"], 3)
        self.assertEqual(summary["shared_read_blocks"], 10)

    def test_redaction_removes_urls_and_host_addresses(self):
        redacted = PERFORMANCE.redact(
            "failed postgres://user:secret@127.0.0.1:9135/postgres connection to server at \"127.0.0.1\", port 9135"
        )
        self.assertNotIn("postgres://", redacted)
        self.assertNotIn("secret", redacted)
        self.assertNotIn("127.0.0.1", redacted)
        self.assertNotIn("9135", redacted)

    def test_measurement_script_prepares_each_statement_once(self):
        statements = PERFORMANCE.load_statements()
        cases = PERFORMANCE.query_cases()
        script = PERFORMANCE.measure_script(statements, cases)
        self.assertEqual(script.count("PREPARE kura_s7_search("), 1)
        self.assertEqual(script.count("PREPARE kura_s7_similarity("), 1)
        self.assertIn("KURA_S7_CASE search_newest_first 1", script)
        self.assertIn("KURA_S7_CASE similarity_later 2", script)
        self.assertIn("plan_cache_mode = force_custom_plan", script)
        self.assertIn("deleted_at IS NULL", script)
        self.assertNotIn("postgresql://", script)

    def test_proof_source_does_not_embed_a_connection_url(self):
        source = SCRIPT.read_text(encoding="utf-8")
        self.assertNotIn("postgresql://", source)
        self.assertNotIn("postgres://", source)
        self.assertNotIn("9135", source)


if __name__ == "__main__":
    unittest.main()
