"""Native CLI commands against generated originals and disposable databases."""
import hashlib
import json
import os
import subprocess
import unittest
import zipfile

import test_library_backup_integration as FIXTURE


@unittest.skipUnless(os.environ.get('KURA_BACKUP_TEST_ADMIN_URL'), 'set an explicitly disposable local admin URL')
class CLIParityDrill(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.RestoreDrill()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.setUp()
        _, self.api = self.fixture.start_server(self.fixture.source, self.fixture.media)
        self.binary = self.fixture.root / 'kura-cli'
        built = subprocess.run(['go', '-C', str(FIXTURE.REPO / 'cli'), 'build', '-o', str(self.binary), './cmd/kura'], capture_output=True, text=True)
        self.assertEqual(built.returncode, 0, built.stderr)

    def cli(self, *args, success=True, token=True):
        env = os.environ.copy()
        env.update(KURA_API_URL=self.api, KURA_HTTP_TIMEOUT='20s')
        env.pop('KURA_API_TOKEN', None)
        if token:
            env['KURA_API_TOKEN'] = 'restore-drill-token'
        result = subprocess.run([str(self.binary), *args], env=env, capture_output=True, text=True, timeout=25)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
            return json.loads(result.stdout)
        self.assertNotEqual(result.returncode, 0, 'unsafe command unexpectedly succeeded')
        return result

    def test_metadata_trash_restore_and_nonoverwriting_exports(self):
        payload = FIXTURE.png((38, 99, 123))
        post = self.fixture.upload(self.api, payload)
        post_id = post['id']
        edited = self.cli('post', 'metadata', '--json', '--version', '0', '--artist', ' CLI artist ', post_id)['posts'][0]
        self.assertEqual((edited['artist'], edited['source'], edited['version']), ('CLI artist', post['source'], 1))
        self.cli('post', 'metadata', '--version', '0', '--source', 'stale', post_id, success=False)
        cleared = self.cli('post', 'metadata', '--json', '--version', '1', '--source', '', post_id)['posts'][0]
        self.assertEqual((cleared['source'], cleared['artist'], cleared['version']), ('', 'CLI artist', 2))
        collection = self.fixture.request(self.api, 'POST', '/api/collections', {'name': 'CLI originals'})
        collection = self.fixture.request(self.api, 'POST', '/api/collections/' + collection['id'] + '/posts', {'post_ids': [post_id]})
        output = self.fixture.root / 'selection.zip'
        receipt = self.cli('export', '--json', '--ids', post_id, '--metadata', '--output', str(output), token=False)
        self.assertEqual((receipt['path'], receipt['bytes']), (str(output), output.stat().st_size))
        self.assertEqual(output.stat().st_mode & 0o777, 0o600)
        with zipfile.ZipFile(output) as bundle:
            self.assertEqual(bundle.read('originals/' + post_id + '.png'), payload)
            self.assertEqual(json.loads(bundle.read('metadata.json'))['posts'][0]['hash'], 'sha256:' + hashlib.sha256(payload).hexdigest())
        original_archive = output.read_bytes()
        self.cli('export', '--ids', post_id, '--output', str(output), success=False)
        self.assertEqual(output.read_bytes(), original_archive)
        target = self.fixture.root / 'collection.zip'
        self.cli('collection', 'export', '--json', '--version', str(collection['version']), '--output', str(target), collection['id'])
        stale = self.fixture.root / 'stale.zip'
        self.cli('collection', 'export', '--version', str(collection['version'] - 1), '--output', str(stale), collection['id'], success=False)
        self.assertFalse(stale.exists())
        self.assertFalse(list(self.fixture.root.glob('.kura-export-*')))
        self.cli('post', 'trash', '--reason', 'CLI fixture', post_id, success=False, token=False)
        trashed = self.cli('post', 'trash', '--json', '--reason', 'CLI fixture', post_id)['posts'][0]
        self.assertEqual(trashed, {'id': post_id, 'changed': True})
        self.cli('post', 'trash-list', '--json', success=False, token=False)
        self.assertEqual(self.cli('post', 'trash-list', '--json', '--limit', '1')['posts'][0]['id'], post_id)
        self.assertEqual(self.fixture.request(self.api, 'GET', '/api/posts')['posts'], [])
        self.assertTrue(self.cli('post', 'restore', '--json', post_id)['posts'][0]['changed'])
        restored = self.fixture.request(self.api, 'GET', '/api/posts/' + post_id)
        self.assertEqual((restored['source'], restored['artist'], restored['metadata_version']), ('', 'CLI artist', 2))
        self.assertEqual(self.fixture.request(self.api, 'GET', post['original_url']), payload)


if __name__ == '__main__':
    unittest.main()
