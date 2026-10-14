"""Tag management acceptance against an owned database and generated media."""

import json
import os
import subprocess
import unittest
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

import test_library_backup_integration as FIXTURE


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set an explicitly disposable local admin URL")
class TagManagementDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.server, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def request(self, method, path, body=None):
        return self.fixture.request(self.api, method, path, body)

    def rejected(self, status, path, body=None, method="POST"):
        with self.assertRaises(HTTPError) as error:
            self.request(method, path, body)
        self.assertEqual(error.exception.code, status)
        error.exception.close()

    def tags(self, post_id, version, add, remove=None):
        return self.request("POST", "/api/posts/tags", {
            "posts": [{"id": post_id, "version": version}],
            "add": add, "remove": remove or [],
        })

    def preview(self, mode, source, target):
        return self.request("POST", "/api/tags/manage/preview", {
            "mode": mode, "source_tag": source, "target_tag": target,
        })

    def apply(self, preview):
        return self.request("POST", "/api/tags/manage/apply", {
            "mode": preview["mode"], "source_tag": preview["source_tag"],
            "target_tag": preview["target_tag"], "expected_version": preview["version"],
        })

    def search(self, query):
        return self.request("GET", "/api/posts?" + urlencode({"q": query}))["posts"]

    def test_rename_merge_redirects_revert_visibility_and_cli(self):
        ids = [self.fixture.upload(self.api, FIXTURE.png(color))["id"]
               for color in ((81, 21, 105), (25, 61, 101), (44, 92, 23))]
        first, hidden, trashed = ids
        self.tags(first, 0, ["before", "old", "after"])
        self.tags(hidden, 0, ["old"])
        self.tags(trashed, 0, ["old", "target"])
        self.request("POST", "/api/posts/metadata", {
            "posts": [{"id": first, "version": 0}], "source": "fixture-source", "artist": "old",
        })
        self.request("POST", "/api/posts/moderation", {
            "posts": [{"id": hidden}], "action": "hide", "reason": "tag privacy fixture",
        })
        self.request("POST", "/api/posts/trash", {"post_ids": [trashed]})

        # The management list is private, including when a caller only uses GET.
        with self.assertRaises(HTTPError) as error:
            urlopen(Request(self.api + "/api/tags/manage"), timeout=5)
        self.assertEqual(error.exception.code, 401)
        error.exception.close()
        listing = self.request("GET", "/api/tags/manage?prefix=old&limit=1")
        self.assertEqual(listing["tags"], [{"tag": "old", "post_count": 3, "visible_post_count": 1}])
        preview = self.preview("rename", "old", "renamed")
        self.assertEqual((preview["affected_post_count"], preview["visible_post_count"]), (3, 1))
        self.rejected(400, "/api/tags/manage/apply", {"mode": "rename", "source_tag": "old", "target_tag": "renamed"})
        self.tags(first, 1, ["late"])
        self.rejected(409, "/api/tags/manage/apply", {
            "mode": "rename", "source_tag": "old", "target_tag": "renamed", "expected_version": preview["version"],
        })
        renamed = self.apply(self.preview("rename", "old", "renamed"))
        self.assertEqual((renamed["affected_post_count"], renamed["visible_post_count"]), (3, 1))
        self.assertTrue(renamed["changed"])
        detail = self.request("GET", "/api/posts/" + first)
        self.assertEqual(detail["tags"], ["before", "renamed", "after", "late"])
        self.assertEqual((detail["source"], detail["artist"], detail["metadata_version"]), ("fixture-source", "old", 1))
        self.assertEqual([post["id"] for post in self.search("tag:old")], [first])
        self.assertEqual(self.search("-tag:old"), [])
        self.assertEqual([post["id"] for post in self.search("artist:old")], [first])
        self.assertEqual(self.request("GET", "/api/tags/suggestions?prefix=target")["tags"], [])
        self.rejected(409, "/api/tags/manage/preview", {"mode": "rename", "source_tag": "old", "target_tag": "again"})

        merged = self.apply(self.preview("merge", "renamed", "target"))
        self.assertEqual(merged["affected_post_count"], 3)
        detail = self.request("GET", "/api/posts/" + first)
        self.assertEqual(detail["tags"], ["before", "target", "after", "late"])
        # Reverting the old snapshot maps retired names to their current target.
        self.request("POST", "/api/posts/tags/revert", {
            "posts": [{"id": first, "version": detail["tag_version"]}], "target_version": 1,
        })
        detail = self.request("GET", "/api/posts/" + first)
        self.assertEqual(detail["tags"], ["before", "target", "after"])
        self.tags(first, detail["tag_version"], ["old", "renamed"])
        self.assertEqual(self.request("GET", "/api/posts/" + first)["tags"], ["before", "target", "after"])

        # The operation covered inaccessible members without reviving them.
        self.rejected(404, "/api/posts/" + hidden, method="GET")
        self.rejected(404, "/api/posts/" + trashed, method="GET")
        rows = json.loads(self.fixture.pg(self.fixture.source, ["-c", "SELECT jsonb_agg(jsonb_build_object('id',id::text,'tags',tags,'hidden',moderation_state='hidden','deleted',deleted_at IS NOT NULL) ORDER BY id) FROM posts"]))
        by_id = {row["id"]: row for row in rows}
        self.assertEqual(by_id[hidden]["tags"], ["target"])
        self.assertEqual(by_id[trashed]["tags"], ["target"])
        self.assertTrue(by_id[hidden]["hidden"])
        self.assertTrue(by_id[trashed]["deleted"])

        binary = self.fixture.root / "kura-cli"
        result = subprocess.run(["go", "-C", str(FIXTURE.REPO / "cli"), "build", "-o", str(binary), "./cmd/kura"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        env = os.environ.copy()
        env.update(KURA_API_TOKEN="restore-drill-token", KURA_API_URL=self.api)
        def cli(*args):
            return subprocess.run([str(binary), "tag", *args], env=env, capture_output=True, text=True)
        preview_cli = cli("rename", "--json", "target", "final")
        self.assertEqual(preview_cli.returncode, 0, preview_cli.stderr)
        version = json.loads(preview_cli.stdout)["version"]
        self.assertEqual(self.request("GET", "/api/posts/" + first)["tags"], ["before", "target", "after"])
        applied_cli = cli("rename", "--version", str(version), "--json", "target", "final")
        self.assertEqual(applied_cli.returncode, 0, applied_cli.stderr)
        self.assertEqual(json.loads(applied_cli.stdout)["affected_post_count"], 3)
        self.assertEqual([post["id"] for post in self.search("tag:old tag:renamed tag:target tag:final")], [first])
        stale_cli = cli("rename", "--version", str(version), "final", "other")
        self.assertNotEqual(stale_cli.returncode, 0)
        self.assertIn("409", stale_cli.stderr)

        # A restore must preserve retired-name redirects and linked operation history.
        self.fixture.stop_server(self.server)
        expected = self.fixture.snapshots(self.fixture.source)
        self.assertEqual(len(expected["tag_operations"]), 3)
        self.assertEqual(len(expected["tag_redirects"]), 3)
        backup, restored_media = self.fixture.root / "tag-backup", self.fixture.root / "restored-media"
        self.fixture.tool(self.fixture.source, "create", "--output", str(backup), "--media-root", str(self.fixture.media), "--offline")
        self.fixture.tool(self.fixture.target, "restore", "--input", str(backup), "--media-root", str(restored_media), "--offline")
        self.assertEqual(self.fixture.snapshots(self.fixture.target), expected)
        _, restored_api = self.fixture.start_server(self.fixture.target, restored_media)
        restored = self.fixture.request(restored_api, "GET", "/api/posts?" + urlencode({"q": "tag:old"}))["posts"]
        self.assertEqual([post["id"] for post in restored], [first])
        self.assertEqual(restored[0]["tags"], ["before", "final", "after"])

    def test_version_bound_literal_paging_and_large_operation_rejection(self):
        post_id = self.fixture.upload(self.api, FIXTURE.png((57, 66, 81)))["id"]
        self.tags(post_id, 0, ["a%one", "a%two", "a_under", "bounded", "target"])
        first = self.request("GET", "/api/tags/manage?" + urlencode({"prefix": "a%", "limit": 1}))
        self.assertEqual([row["tag"] for row in first["tags"]], ["a%one"])
        self.assertTrue(first["next_cursor"])
        second = self.request("GET", "/api/tags/manage?" + urlencode({"prefix": "a%", "limit": 1, "cursor": first["next_cursor"]}))
        self.assertEqual([row["tag"] for row in second["tags"]], ["a%two"])
        self.assertIsNone(second["next_cursor"])
        self.tags(post_id, 1, ["a%three"])
        self.rejected(409, "/api/tags/manage?" + urlencode({"prefix": "a%", "limit": 1, "cursor": first["next_cursor"]}), method="GET")
        # Only fixture rows are cloned. Both preview and apply must stop at the bound.
        self.fixture.pg(self.fixture.source, ["-c", """
INSERT INTO posts (preview_url,original_url,media_type,width,height,source,artist,hash,file_size,tags,search_text)
SELECT preview_url,original_url,media_type,width,height,source,artist,hash,file_size,tags,search_text
FROM posts CROSS JOIN generate_series(1,1000) WHERE id=%s
""" % int(post_id)])
        body = {"mode": "merge", "source_tag": "bounded", "target_tag": "target"}
        self.rejected(400, "/api/tags/manage/preview", body)
        version = self.request("GET", "/api/tags/manage")["version"]
        self.rejected(400, "/api/tags/manage/apply", dict(body, expected_version=version))
        self.assertEqual(self.request("GET", "/api/tags/manage")["version"], version)
        self.assertEqual(self.fixture.pg(self.fixture.source, ["-c", "SELECT count(*) FROM posts WHERE 'bounded'=ANY(tags)"]), "1001")


if __name__ == "__main__":
    unittest.main()
