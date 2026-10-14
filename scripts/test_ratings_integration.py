"""Opt-in rating API/CLI acceptance using generated media and owned databases."""

import json
import os
import subprocess
import unittest
from urllib.error import HTTPError

import test_library_backup_integration as FIXTURE

png, REPO = FIXTURE.png, FIXTURE.REPO


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class RatingsDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def request(self, method, path, body=None):
        return self.fixture.request(self.api, method, path, body)

    def rejected(self, status, body):
        with self.assertRaises(HTTPError) as error:
            self.request("POST", "/api/posts/ratings", body)
        self.assertEqual(error.exception.code, status)
        error.exception.close()

    def test_default_atomic_bulk_history_pagination_and_cli(self):
        first = self.fixture.upload(self.api, png((31, 93, 122)))
        second = self.fixture.upload(self.api, png((87, 13, 119)))
        self.assertEqual((first["rating"], first["rating_version"], first["rating_history"]), ("unrated", 0, []))
        post_id, second_id = first["id"], second["id"]
        body = {"posts": [{"id": post_id, "version": 0}], "rating": "safe"}
        for malformed in (
            {"posts": [{"id": post_id}], "rating": "safe"},
            {"posts": [{"id": post_id, "version": None}], "rating": "safe"},
            {"posts": [{"id": " " + post_id, "version": 0}], "rating": "safe"},
            {"posts": body["posts"], "rating": "SAFE"},
            {"posts": body["posts"], "rating": "safe", "extra": True},
        ):
            self.rejected(400, malformed)
        self.rejected(409, {"posts": [{"id": second_id, "version": 0}, {"id": post_id, "version": 1}], "rating": "explicit"})
        self.assertEqual(self.request("GET", "/api/posts/" + second_id)["rating_version"], 0)
        changed = self.request("POST", "/api/posts/ratings", body)["posts"][0]
        self.assertEqual(changed, {"id": post_id, "version": 1, "rating": "safe", "changed": True})
        body["posts"][0]["version"] = 1
        noop = self.request("POST", "/api/posts/ratings", body)["posts"][0]
        self.assertFalse(noop["changed"])
        self.assertEqual(noop["version"], 1)
        self.rejected(409, {"posts": [{"id": post_id, "version": 0}], "rating": "safe"})

        # Newest fifty entries remain ordered, and classification never hides media.
        for version in range(1, 56):
            rating = ("explicit", "questionable", "unrated", "safe")[version % 4]
            self.request("POST", "/api/posts/ratings", {"posts": [{"id": post_id, "version": version}], "rating": rating})
        detail = self.request("GET", "/api/posts/" + post_id)
        self.assertEqual(detail["rating_version"], 56)
        self.assertEqual([entry["version"] for entry in detail["rating_history"]], list(range(56, 6, -1)))
        self.assertEqual((detail["tag_version"], detail["metadata_version"], detail["reaction_version"]), (0, 0, 0))
        self.assertEqual(self.request("GET", detail["original_url"]), png((31, 93, 122)))

        # A hidden member rejects the entire batch without changing visible members.
        self.request("POST", "/api/posts/moderation", {"posts": [{"id": second_id}], "action": "hide", "reason": "rating fixture"})
        self.rejected(404, {"posts": [{"id": post_id, "version": 56}, {"id": second_id, "version": 0}], "rating": "explicit"})
        self.assertEqual(self.request("GET", "/api/posts/" + post_id)["rating_version"], 56)

        # Clone only fixture rows to exercise the real 100-target bound and paging.
        ids = self.fixture.pg(self.fixture.source, ["-c", """
INSERT INTO posts (preview_url, original_url, media_type, width, height, source, artist, hash, file_size, tags, search_text)
SELECT preview_url, original_url, media_type, width, height, source, artist, hash, file_size, tags, search_text
FROM posts CROSS JOIN generate_series(1,120) WHERE id=%s RETURNING id;
""" % int(post_id)]).splitlines()
        ids = [value for value in ids if value.isdigit()]
        self.assertEqual(len(ids), 120)
        targets = [{"id": value, "version": 0} for value in ids]
        self.rejected(400, {"posts": targets[:101], "rating": "questionable"})
        response = self.request("POST", "/api/posts/ratings", {"posts": targets[:100], "rating": "questionable"})
        self.assertEqual(len(response["posts"]), 100)
        seen, cursor = [], None
        while True:
            query = "/api/posts?q=rating%3Aquestionable&limit=17" + ("&cursor=" + cursor if cursor else "")
            page = self.request("GET", query)
            self.assertTrue(all(post["rating"] == "questionable" and post["rating_version"] == 1 for post in page["posts"]))
            seen.extend(post["id"] for post in page["posts"])
            cursor = page["next_cursor"]
            if not cursor:
                break
            self.assertLess(len(seen), 130, "rating paging did not terminate")
        self.assertEqual(set(seen), set(ids[:100]))
        self.assertEqual(len(seen), 100)

        binary = self.fixture.root / "kura-cli"
        built = subprocess.run(["go", "-C", str(REPO / "cli"), "build", "-o", str(binary), "./cmd/kura"], capture_output=True, text=True)
        self.assertEqual(built.returncode, 0, built.stderr)
        env = os.environ.copy()
        env["KURA_API_TOKEN"] = "restore-drill-token"
        result = subprocess.run([str(binary), "post", "rating", "--api-url", self.api, "--version", "0", "--json", ids[-1], "explicit"], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["posts"][0]["rating"], "explicit")
        stale = subprocess.run([str(binary), "post", "rating", "--api-url", self.api, "--version", "0", ids[-1], "safe"], env=env, capture_output=True, text=True)
        self.assertNotEqual(stale.returncode, 0)
        self.assertIn("409", stale.stderr)
        self.assertEqual(self.request("GET", "/api/posts/" + ids[-1])["rating"], "explicit")


if __name__ == "__main__":
    unittest.main()
