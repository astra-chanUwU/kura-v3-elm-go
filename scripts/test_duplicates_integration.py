"""Exact-copy paging and privacy using generated originals and owned databases."""
import os
import unittest
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import urlopen
import json

import test_library_backup_integration as FIXTURE


@unittest.skipUnless(os.environ.get('KURA_BACKUP_TEST_ADMIN_URL'), 'set an explicitly disposable local admin URL')
class DuplicatesDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)

    def public(self, path):
        with urlopen(self.api + path, timeout=5) as response:
            return json.load(response)

    def rejected(self, status, path):
        with self.assertRaises(HTTPError) as error:
            self.public(path)
        self.assertEqual(error.exception.code, status)
        error.exception.close()

    def clone(self, post_id, count):
        result = self.fixture.pg(self.fixture.source, ['-c', '''
INSERT INTO posts (preview_url, original_url, media_type, width, height, tags, source, artist, hash, file_size, search_text)
SELECT preview_url, original_url, media_type, width, height, tags, source, artist, hash, file_size, search_text
FROM posts CROSS JOIN generate_series(1,%d) WHERE id=%d RETURNING id
''' % (count, int(post_id))])
        return [value for value in result.splitlines() if value.isdigit()]

    def test_group_member_keysets_visibility_and_recovery(self):
        first, second = [self.fixture.upload(self.api, FIXTURE.png(color)) for color in ((33, 70, 91), (93, 24, 79))]
        ready = self.fixture.wait_for_thumbnail(self.api, first['id'])
        first_copies = self.clone(first['id'], 124)
        second_copies = self.clone(second['id'], 3)
        hidden, trashed = first_copies[-2:]
        self.fixture.request(self.api, 'POST', '/api/posts/moderation', {'posts': [{'id': hidden}], 'action': 'hide', 'reason': 'duplicate fixture'})
        self.fixture.request(self.api, 'POST', '/api/posts/trash', {'post_ids': [trashed]})
        invalid = self.clone(second['id'], 2)
        self.fixture.pg(self.fixture.source, ['-c', "UPDATE posts SET hash='legacy-invalid' WHERE id IN (" + ','.join(invalid) + ')'])
        groups = self.public('/api/posts/duplicates?limit=1')
        self.assertEqual(len(groups['groups']), 1)
        self.assertIsNotNone(groups['next_cursor'])
        last = self.public('/api/posts/duplicates?' + urlencode({'limit': 1, 'cursor': groups['next_cursor']}))
        self.assertIsNone(last['next_cursor'])
        all_groups = groups['groups'] + last['groups']
        self.assertEqual([g['hash'] for g in all_groups], sorted([first['hash'], second['hash']]))
        by_hash = {g['hash']: g for g in all_groups}
        self.assertEqual((by_hash[first['hash']]['count'], by_hash[second['hash']]['count']), (123, 4))
        self.assertTrue(all(len(g['posts']) <= 3 for g in all_groups))
        self.assertEqual([p['id'] for p in by_hash[second['hash']]['posts']], [second['id'], *second_copies[:2]])
        self.assertEqual(by_hash[first['hash']]['posts'][0]['preview_url'], ready['preview_url'])
        path = '/api/posts/duplicates/' + quote(first['hash'], safe='')
        members = self.public(path + '?limit=60')
        member_cursor = members['next_cursor']
        ids = [row['id'] for row in members['posts']]
        while members['next_cursor']:
            members = self.public(path + '?' + urlencode({'limit': 60, 'cursor': members['next_cursor']}))
            ids.extend(row['id'] for row in members['posts'])
        self.assertEqual(len(ids), 123)
        self.assertEqual(ids, sorted(set(ids), key=int))
        self.assertNotIn(hidden, ids);self.assertNotIn(trashed, ids)
        self.rejected(400, '/api/posts/duplicates/' + quote(second['hash'], safe='') + '?' + urlencode({'cursor': member_cursor}))
        self.rejected(400, '/api/posts/duplicates?' + urlencode({'cursor': member_cursor}))
        self.rejected(400, path + '?' + urlencode({'cursor': groups['next_cursor']}))
        for invalid_path in ('/api/posts/duplicates?limit=31', '/api/posts/duplicates/not-a-hash', path + '?limit=61'):
            self.rejected(400, invalid_path)
        # Removing copies is manual and recoverable; no group read deletes anything.
        second_ids = [second['id'], *second_copies]
        self.fixture.request(self.api, 'POST', '/api/posts/trash', {'post_ids': second_ids[1:]})
        second_path = '/api/posts/duplicates/' + quote(second['hash'], safe='')
        self.rejected(404, second_path)
        self.assertEqual(len(self.public('/api/posts/duplicates')['groups']), 1)
        self.fixture.request(self.api, 'POST', '/api/posts/restore', {'post_ids': second_ids[1:]})
        self.assertEqual(self.public(second_path)['count'], 4)
        self.assertEqual(self.fixture.request(self.api, 'GET', first['original_url']), FIXTURE.png((33, 70, 91)))


if __name__ == '__main__':
    unittest.main()
