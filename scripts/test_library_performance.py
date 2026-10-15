"""Focused tests for the disposable library-performance harness guards."""

import importlib.util
from pathlib import Path
import unittest


SCRIPT = Path(__file__).with_name("library-performance.py")
SPEC = importlib.util.spec_from_file_location("kura_library_performance", SCRIPT)
PERFORMANCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PERFORMANCE)


class LibraryPerformanceHarnessTests(unittest.TestCase):
    def test_local_admin_url_is_moved_to_environment_fields(self):
        environment, database = PERFORMANCE.local_admin_environment(
            "postgresql://perf-user:sensitive-value@127.0.0.1:55432/postgres?sslmode=disable"
        )
        self.assertEqual(database, "postgres")
        self.assertEqual(environment["PGHOST"], "127.0.0.1")
        self.assertEqual(environment["PGPORT"], "55432")
        self.assertEqual(environment["PGUSER"], "perf-user")
        self.assertEqual(environment["PGPASSWORD"], "sensitive-value")

    def test_nonlocal_database_url_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "local postgresql URL"):
            PERFORMANCE.local_admin_environment("postgresql://u:p@example.invalid/postgres")

    def test_unexpected_connection_parameters_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "local postgresql URL"):
            PERFORMANCE.local_admin_environment("postgresql://u:p@localhost/postgres?options=secret")

    def test_generated_database_name_has_only_owned_prefix_and_random_token(self):
        self.assertEqual(PERFORMANCE.generated_database_name("0123456789abcdef01234567"), "kura_perf_0123456789abcdef01234567")
        with self.assertRaisesRegex(ValueError, "generated database name"):
            PERFORMANCE.generated_database_name("existing_database")

    def test_representative_queries_match_supported_sizes(self):
        self.assertEqual([name for name, _sql in PERFORMANCE.query_cases(10_000)], [
            "search_rare_term",
            "deep_keyset_page",
            "collection_deep_keyset_page",
            "thumbnail_overlay_for_page",
        ])
        self.assertIn("cp.position > 9000", PERFORMANCE.query_cases(10_000)[2][1])
        self.assertIn("/synthetic/media/", PERFORMANCE.seed_sql(10_000))


if __name__ == "__main__":
    unittest.main()
