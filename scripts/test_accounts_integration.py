"""Private-library acceptance using generated accounts and disposable databases."""
import http.cookiejar
import json
import os
import socket
import subprocess
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import HTTPCookieProcessor, Request, build_opener

import test_library_backup_integration as FIXTURE


@unittest.skipUnless(os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "set a disposable local admin URL")
class AccountsDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        self.password = "generated-fixture-password-53"
        env = self.fixture.environment(self.fixture.source)
        bootstrap = subprocess.run(
            ["go", "-C", str(FIXTURE.REPO / "server"), "run", "./cmd/kura-account", "bootstrap", "--handle", "owner"],
            input=self.password + "\n", env=env, capture_output=True, text=True,
        )
        self.assertEqual(bootstrap.returncode, 0, "disposable account bootstrap failed")
        repeated = subprocess.run(
            ["go", "-C", str(FIXTURE.REPO / "server"), "run", "./cmd/kura-account", "bootstrap", "--handle", "secondowner"],
            input=self.password + "\n", env=env, capture_output=True, text=True,
        )
        self.assertNotEqual(repeated.returncode, 0, "second bootstrap must fail")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        self.api = "http://127.0.0.1:" + str(port)
        env.update(HTTP_ADDR="127.0.0.1:" + str(port), MEDIA_ROOT=str(self.fixture.media),
                   KURA_API_TOKEN="restore-drill-token", KURA_PRIVATE_LIBRARY="true",
                   KURA_AUTH_ALLOW_HTTP_LOCAL="true", KURA_AUTH_TRUSTED_ORIGIN=self.api)
        process = subprocess.Popen([str(self.fixture.binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.fixture.servers.append(process)
        self.anonymous = self.client()
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                self.call(self.anonymous, "GET", "/health")
                break
            except URLError:
                time.sleep(0.05)
        else:
            self.fail("disposable private API did not become ready")

    @staticmethod
    def client():
        return build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, client, method, path, body=None, expected=200, origin=True, bearer=False):
        headers = {}
        if origin:
            headers["Origin"] = self.api
        if bearer:
            headers["Authorization"] = "Bearer restore-drill-token"
        if body is not None:
            headers["Content-Type"] = "application/json"
            body = json.dumps(body).encode()
        request = Request(self.api + path, data=body, headers=headers, method=method)
        try:
            response = client.open(request, timeout=10)
        except HTTPError as error:
            response = error
        with response:
            self.assertEqual(response.code, expected, "unexpected response for " + method + " " + path)
            payload = response.read()
            if "application/json" in response.headers.get("Content-Type", ""):
                return json.loads(payload)
            return payload

    def login(self, handle):
        client = self.client()
        session = self.call(client, "POST", "/api/auth/login", {"handle": handle, "password": self.password})
        self.assertTrue(session["authenticated"])
        self.assertNotIn("token", session)
        return client, session

    def test_roles_private_media_csrf_revocation_and_actor_searches(self):
        session = self.call(self.anonymous, "GET", "/api/auth/session")
        self.assertTrue(session["private_mode"])
        self.assertFalse(session["authenticated"])
        self.assertFalse(self.call(self.anonymous, "GET", "/api/capabilities")["can_write"])
        for path in ("/api/posts", "/api/collections", "/api/posts/export", "/media/original/missing.png"):
            self.call(self.anonymous, "GET", path, expected=401)
        admin, owner_session = self.login("owner")
        self.assertTrue(owner_session["is_admin"])
        ids = {}
        for role in ("viewer", "editor"):
            result = self.call(admin, "POST", "/api/auth/users", {"handle": role, "password": self.password, "role": role}, expected=201)
            ids[role] = result["user"]["id"]
        viewer, viewer_session = self.login("viewer")
        editor, editor_session = self.login("editor")
        self.assertFalse(viewer_session["can_write"])
        self.assertTrue(editor_session["can_write"])
        self.assertFalse(editor_session["can_moderate"])
        self.assertFalse(self.call(viewer, "GET", "/api/capabilities")["can_write"])
        self.assertTrue(self.call(editor, "GET", "/api/capabilities")["can_write"])
        self.call(viewer, "GET", "/api/posts")
        self.call(viewer, "POST", "/api/collections", {"name": "Forbidden"}, expected=403)
        self.call(editor, "POST", "/api/collections", {"name": "No origin"}, expected=403, origin=False)
        self.call(editor, "POST", "/api/collections", {"name": "Editor collection"}, expected=201)
        for path in ("/api/posts/moderation", "/api/auth/users", "/api/recovery/backups"):
            self.call(editor, "GET", path, expected=403)
        # Non-ambient deployment credentials still work without Origin.
        self.call(self.anonymous, "POST", "/api/collections", {"name": "CLI collection"}, expected=201, origin=False, bearer=True)
        own = self.call(admin, "POST", "/api/saved-searches", {"name": "Owner search", "query": "owner-tag"}, expected=201)
        self.call(editor, "POST", "/api/saved-searches", {"name": "Editor search", "query": "editor-tag"}, expected=201)
        self.assertEqual([r["name"] for r in self.call(editor, "GET", "/api/saved-searches")["saved_searches"]], ["Editor search"])
        self.call(editor, "DELETE", "/api/saved-searches/" + own["id"], expected=404)
        post = self.fixture.upload(self.api, FIXTURE.png((51, 73, 129)))
        ready = self.fixture.wait_for_thumbnail(self.api, post["id"])
        for path in (post["original_url"], ready["preview_url"]):
            self.call(self.anonymous, "GET", path, expected=401)
            self.assertTrue(self.call(viewer, "GET", path))
        self.call(admin, "POST", "/api/posts/moderation", {"posts": [{"id": post["id"]}], "action": "hide", "reason": "Private fixture"})
        self.call(viewer, "GET", post["original_url"], expected=404)
        self.call(editor, "GET", "/api/posts/" + post["id"] + "/moderation/preview", expected=403)
        self.assertTrue(self.call(admin, "GET", "/api/posts/" + post["id"] + "/moderation/preview"))
        self.call(admin, "PATCH", "/api/auth/users/" + str(ids["viewer"]), {"disabled": True})
        self.call(viewer, "GET", "/api/posts", expected=401)
        self.call(admin, "POST", "/api/auth/logout", {})
        self.call(admin, "GET", "/api/posts", expected=401)


if __name__ == "__main__":
    unittest.main()
