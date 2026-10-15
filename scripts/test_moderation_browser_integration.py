"""Paged moderation and protected previews against generated disposable media."""
import json
import os
import unittest
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen
import test_library_backup_integration as FIXTURE


@unittest.skipUnless(os.environ.get('KURA_BACKUP_TEST_ADMIN_URL'), 'set a disposable local admin URL')
class ModerationBrowserDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def raw(self, path, token=True):
        headers = {'Authorization': 'Bearer restore-drill-token'} if token else {}
        with urlopen(Request(self.api + path, headers=headers), timeout=8) as response:
            return response.status, dict(response.headers), response.read()

    def rejected(self, path, status, token=True):
        with self.assertRaises(HTTPError) as error:
            self.raw(path, token)
        self.assertEqual(error.exception.code, status)
        error.exception.close()

    def test_hidden_preview_queue_paging_history_and_recovery(self):
        post = self.fixture.upload(self.api, FIXTURE.png((88, 29, 118)))
        post_id = post['id']
        ready = self.fixture.wait_for_thumbnail(self.api, post_id)
        preview_path = '/api/posts/' + post_id + '/moderation/preview'
        self.rejected(preview_path, 401, token=False)
        self.rejected('/api/posts/999999/moderation/preview', 401, token=False)
        status, headers, thumbnail = self.raw(preview_path)
        self.assertEqual(status, 200)
        self.assertEqual(headers['Content-Type'], 'image/jpeg')
        self.assertIn('no-store', headers['Cache-Control'])
        self.assertTrue(thumbnail.startswith(b'\xff\xd8'))
        copies = self.fixture.pg(self.fixture.source, ['-c', '''
INSERT INTO posts (preview_url,original_url,media_type,width,height,tags,source,artist,hash,file_size,search_text)
SELECT preview_url,original_url,media_type,width,height,tags,source,artist,hash,file_size,search_text
FROM posts CROSS JOIN generate_series(1,64) WHERE id=%s RETURNING id
''' % int(post_id)]).splitlines()
        ids = [post_id] + [value for value in copies if value.isdigit()]
        self.assertEqual(len(ids), 65)
        self.fixture.request(self.api, 'POST', '/api/posts/moderation', {'posts': [{'id': value} for value in ids], 'action': 'hide', 'reason': 'Browser moderation fixture'})
        self.rejected(post['original_url'], 404, token=False)
        self.rejected(ready['preview_url'], 404, token=False)
        self.assertEqual(self.raw(preview_path)[2], thumbnail)
        self.rejected('/api/posts/moderation?state=hidden', 401, token=False)
        page = self.fixture.request(self.api, 'GET', '/api/posts/moderation?state=hidden&limit=20')
        first_cursor = page['next_cursor']
        found = []
        while True:
            found += [row['id'] for row in page['posts']]
            if not page['next_cursor']:
                break
            page = self.fixture.request(self.api, 'GET', '/api/posts/moderation?' + urlencode({'state': 'hidden', 'limit': 20, 'cursor': page['next_cursor']}))
        self.assertEqual(found, sorted(ids, key=int, reverse=True))
        self.assertEqual(len(found), len(set(found)))
        self.rejected('/api/posts/moderation?' + urlencode({'state': 'published', 'cursor': first_cursor}), 400)
        history = self.fixture.request(self.api, 'GET', '/api/posts/' + post_id + '/moderation')['actions']
        self.assertEqual((history[0]['actor'], history[0]['reason'], history[0]['new_state']), ('system', 'Browser moderation fixture', 'hidden'))
        self.fixture.request(self.api, 'POST', '/api/posts/moderation', {'posts': [{'id': post_id}], 'action': 'approve', 'reason': 'Reviewed'})
        self.assertEqual(self.raw(post['original_url'], token=False)[0], 200)
        self.fixture.request(self.api, 'POST', '/api/posts/trash', {'post_ids': [post_id]})
        self.rejected(preview_path, 404)
        self.assertNotIn(post_id, [row['id'] for row in self.fixture.request(self.api, 'GET', '/api/posts/moderation?state=published')['posts']])


if __name__ == '__main__':
    unittest.main()
