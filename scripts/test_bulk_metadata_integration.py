"""Bulk metadata acceptance against generated images and an owned database."""

import os
import unittest
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

import test_library_backup_integration as FIXTURE


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class BulkMetadataDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def request(self, method, path, body=None):
        return self.fixture.request(self.api, method, path, body)

    def rejected(self, code, path, body):
        with self.assertRaises(HTTPError) as error:
            self.request("POST", path, body)
        self.assertEqual(error.exception.code, code)
        error.exception.close()

    def test_mixed_values_clear_omit_atomic_conflict_and_bounds(self):
        first, second = [self.fixture.upload(self.api, FIXTURE.png(color))["id"]
                         for color in ((19, 51, 87), (81, 36, 29))]
        for post_id, source, artist in ((first, "source-one", "artist-one"), (second, "source-two", "artist-two")):
            self.request("POST", "/api/posts/metadata", {"posts": [{"id": post_id, "version": 0}], "source": source, "artist": artist})
        read_path = "/api/posts/metadata/read"
        with self.assertRaises(HTTPError) as error:
            urlopen(Request(self.api + read_path, data=b'{"post_ids":["1"]}', headers={"Content-Type": "application/json"}, method="POST"), timeout=5)
        self.assertEqual(error.exception.code, 401)
        error.exception.close()
        base = self.request("POST", read_path, {"post_ids": [second, first]})["posts"]
        self.assertEqual([row["id"] for row in base], [second, first])
        self.assertEqual([row["version"] for row in base], [1, 1])
        self.assertEqual([row["source"] for row in base], ["source-two", "source-one"])
        targets = [{"id": row["id"], "version": row["version"]} for row in base]
        cleared = self.request("POST", "/api/posts/metadata", {"posts": targets, "source": ""})["posts"]
        self.assertEqual([row["source"] for row in cleared], ["", ""])
        self.assertEqual([row["artist"] for row in cleared], ["artist-two", "artist-one"])
        self.assertTrue(all(row["changed"] and row["version"] == 2 for row in cleared))
        detail = self.request("GET", "/api/posts/" + first)
        self.assertEqual(len(detail["metadata_history"]), 2)
        self.assertEqual((detail["tag_version"], detail["reaction_version"], detail["rating_version"]), (0, 0, 0))

        # A stale member rejects the whole batch, including members with valid versions.
        self.rejected(409, "/api/posts/metadata", {"posts": [{"id": first, "version": 2}, {"id": second, "version": 1}], "artist": "shared"})
        self.assertEqual(self.request("GET", "/api/posts/" + first)["artist"], "artist-one")
        # Explicit zero versions are valid for a full 100-image edit.
        clone_ids = self.fixture.pg(self.fixture.source, ["-c", """
INSERT INTO posts (preview_url,original_url,media_type,width,height,tags,source,artist,search_text)
SELECT preview_url,original_url,media_type,width,height,tags,'cloned-source','cloned-artist','cloned-source cloned-artist'
FROM posts CROSS JOIN generate_series(1,101) WHERE id=%s RETURNING id
""" % int(first)]).splitlines()
        clone_ids = [value for value in clone_ids if value.isdigit()]
        self.assertEqual(len(clone_ids), 101)
        self.rejected(400, read_path, {"post_ids": clone_ids})
        self.rejected(400, read_path, {"post_ids": [first, first]})
        self.rejected(400, read_path, {"post_ids": [" " + first]})
        read = self.request("POST", read_path, {"post_ids": clone_ids[:100]})["posts"]
        self.assertTrue(all(row["version"] == 0 for row in read))
        changed = self.request("POST", "/api/posts/metadata", {
            "posts": [{"id": row["id"], "version": row["version"]} for row in read], "source": "bulk-source",
        })["posts"]
        self.assertEqual(len(changed), 100)
        self.assertTrue(all(row["source"] == "bulk-source" and row["artist"] == "cloned-artist" and row["version"] == 1 for row in changed))
        matches = self.request("GET", "/api/posts?" + urlencode({"q": "source:bulk-source", "limit": 60}))
        self.assertEqual(len(matches["posts"]), 60)
        self.assertIsNotNone(matches["next_cursor"])
        hidden = clone_ids[-1]
        self.request("POST", "/api/posts/moderation", {"posts": [{"id": hidden}], "action": "hide", "reason": "bulk snapshot fixture"})
        self.rejected(404, read_path, {"post_ids": [first, hidden]})
        self.request("POST", "/api/posts/trash", {"post_ids": [second]})
        self.rejected(404, read_path, {"post_ids": [first, second]})


if __name__ == "__main__":
    unittest.main()
