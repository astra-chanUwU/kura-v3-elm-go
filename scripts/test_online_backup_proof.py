"""Proof tests that do not connect to a database or read a library."""

import hashlib
import importlib.util
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest import mock


PROOF_PATH = Path(__file__).with_name("online_backup_proof.py")
SPEC = importlib.util.spec_from_file_location("online_backup_proof", PROOF_PATH)
proof = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(proof)
backup = proof.backup


class MemoryHold:
    def __init__(self, document, dump=b"fake custom dump", on_dump=None):
        self.document = document
        self.snapshot_id = "00000001-00000001-1"
        self.busy = False
        self.closed = False
        self.dumped = False
        self._dump = dump
        self._on_dump = on_dump

    def dump_to(self, path):
        self.dumped = True
        if self._on_dump is not None:
            self._on_dump(path)
            return
        path.write_bytes(self._dump)

    def close(self):
        self.closed = True


class OnlineBackupProofTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.media = self.root / "media"
        self.media.mkdir()
        self._env = mock.patch.dict(os.environ, {
            "S3_ENDPOINT": "", "AWS_S3_ENDPOINT": "", "S3_BUCKET": "", "AWS_S3_BUCKET": "",
            "S3_URL_PREFIX": "", "MEDIA_URL_PREFIX": "", "S3_PUBLIC_URL_PREFIX": "",
        })
        self._env.start()
        self.addCleanup(self._env.stop)

    def document_for(self, files, *, hidden=(), trashed=(), variants=()):
        posts = []
        for index, (key, data) in enumerate(files.items(), start=1):
            digest = hashlib.sha256(data).hexdigest()
            url = "/media/" + key
            posts.append({
                "id": index,
                "original": url,
                "preview": url,
                "hash": "sha256:" + digest,
                "file_size": len(data),
                "moderation_state": "hidden" if key in hidden else "published",
                "trashed": key in trashed,
            })
            proof.link_immutable(self.media, key, data)
        variant_rows = []
        for key, data, post_id in variants:
            proof.link_immutable(self.media, key, data)
            variant_rows.append({
                "post_id": post_id,
                "object_key": key,
                "url": "/media/" + key,
                "file_size": len(data),
                "status": "ready",
            })
        return {
            "posts": posts,
            "variants": variant_rows,
            "job_keys": [key for key, _ in files.items()],
        }

    def hold_for(self, document, **kwargs):
        hold = MemoryHold(document, **kwargs)

        def opener(_protocol):
            return hold
        return opener, hold

    def backup(self, document, name="proof-backup", **kwargs):
        opener, hold = self.hold_for(document)
        output = self.root / name
        kwargs.setdefault("allow_platform_rename", True)
        result = proof.run_online_backup(self.media, output, opener, **kwargs)
        self.assertTrue(hold.closed)
        return result, hold

    def assert_unpublished(self, name="proof-backup"):
        self.assertFalse((self.root / name).exists())
        self.assertEqual(list(self.root.glob(proof.STAGING_PREFIX + "*")), [])
        self.assertEqual(list(self.root.glob(".kura-proof-snap-*")), [])

    def test_link_put_keeps_the_first_complete_object(self):
        key = "uploads/same.bin"
        self.assertTrue(proof.link_immutable(self.media, key, b"first-complete"))
        self.assertFalse(proof.link_immutable(self.media, key, b"second-different"))
        self.assertEqual((self.media / "uploads" / "same.bin").read_bytes(), b"first-complete")

    def test_parent_directory_symlink_cannot_supply_backup_media(self):
        document = self.document_for({"uploads/a.bin": b"fixture"})
        (self.media / "uploads" / "a.bin").unlink()
        (self.media / "uploads").rmdir()
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "a.bin").write_bytes(b"fixture")
        try:
            (self.media / "uploads").symlink_to(outside, target_is_directory=True)
        except OSError:
            self.skipTest("directory symlinks are unavailable")
        opener, hold = self.hold_for(document)
        with self.assertRaisesRegex(backup.BackupError, "symlink"):
            proof.run_online_backup(self.media, self.root / "linked", opener)
        self.assertTrue(hold.closed)
        self.assert_unpublished("linked")

    def test_concurrent_links_do_not_tear_a_key(self):
        key = "uploads/raced.bin"
        payloads = [bytes([byte]) * 200_000 for byte in (1, 2)]
        barrier = threading.Barrier(2)

        def publish(data):
            barrier.wait()
            proof.link_immutable(self.media, key, data)

        threads = [threading.Thread(target=publish, args=(data,)) for data in payloads]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()
        self.assertIn((self.media / "uploads" / "raced.bin").read_bytes(), payloads)

    def test_snapshot_keeps_hidden_trash_and_ready_variants_not_a_walk(self):
        files = {"uploads/hidden.png": b"hide", "uploads/trashed.png": b"trash"}
        document = self.document_for(
            files, hidden={"uploads/hidden.png"}, trashed={"uploads/trashed.png"},
            variants=[("derivatives/thumb-320/hidden.jpg", b"jpg", 1)],
        )
        (self.media / "orphan.bin").write_bytes(b"orphan")
        pending = self.media / "uploads"
        (pending / ".upload-partial").write_bytes(b"partial")
        proof.link_immutable(self.media, "uploads/pending.bin", b"not-committed")
        result, _hold = self.backup(document)
        backup.verify_backup(result.output)
        self.assertEqual(result.keys, (
            "derivatives/thumb-320/hidden.jpg",
            "uploads/hidden.png",
            "uploads/trashed.png",
        ))
        walked = {path.relative_to(self.media).as_posix() for path in self.media.rglob("*") if path.is_file()}
        self.assertTrue(set(result.keys) < walked)
        self.assertNotIn("orphan.bin", result.keys)
        self.assertNotIn("uploads/.upload-partial", result.keys)
        self.assertNotIn("uploads/pending.bin", result.keys)
        self.assertNotRegex(proof.STAGING_PREFIX + "abc", r"^[A-Za-z0-9]")

    def test_publication_order_snapshot_differs_from_a_later_walk(self):
        root = self.media
        (root / "orphan.bin").write_bytes(b"orphan")
        (root / "uploads").mkdir()
        (root / "uploads" / ".upload-partial").write_bytes(b"partial")
        self.assertTrue(proof.link_immutable(root, "uploads/aaa.bin", b"original-a"))
        self.assertTrue(proof.link_immutable(root, "derivatives/thumb-320/aaa.jpg", b"thumb"))
        snapshot = {"uploads/aaa.bin", "derivatives/thumb-320/aaa.jpg"}
        self.assertTrue(proof.link_immutable(root, "uploads/late.bin", b"late"))
        self.assertTrue(proof.link_immutable(root, "uploads/pending.bin", b"pending"))
        walked = {path.relative_to(root).as_posix() for path in root.rglob("*") if path.is_file()}
        self.assertTrue(snapshot < walked)
        self.assertNotIn("uploads/late.bin", snapshot)
        self.assertNotIn("uploads/pending.bin", snapshot)

    def test_remote_or_conflicting_references_publish_nothing(self):
        document = self.document_for({"uploads/a.bin": b"a"})
        document["posts"][0]["preview"] = "https://cdn.example/a.bin"
        opener, hold = self.hold_for(document)
        with self.assertRaisesRegex(backup.BackupError, "remote media"):
            proof.run_online_backup(self.media, self.root / "remote", opener, allow_platform_rename=True)
        self.assertTrue(hold.closed)
        self.assert_unpublished("remote")

        document = self.document_for({"uploads/b.bin": b"b"})
        document["posts"].append(dict(document["posts"][0], hash="sha256:" + "ab" * 32))
        opener, _hold = self.hold_for(document)
        with self.assertRaisesRegex(proof.ProofError, "hashes disagree"):
            proof.run_online_backup(self.media, self.root / "conflict", opener, allow_platform_rename=True)
        self.assert_unpublished("conflict")

    def test_dump_missing_corrupt_interrupt_and_disk_full_publish_nothing(self):
        document = self.document_for({"uploads/a.bin": b"alpha"})

        def fail_dump(path):
            raise proof.ProofError("synthetic dump failure")

        opener, hold = self.hold_for(document, on_dump=fail_dump)
        with self.assertRaisesRegex(proof.ProofError, "synthetic dump failure"):
            proof.run_online_backup(self.media, self.root / "dump", opener, allow_platform_rename=True)
        self.assertTrue(hold.dumped)
        self.assert_unpublished("dump")

        missing = self.document_for({"uploads/gone.bin": b"gone"})
        (self.media / "uploads" / "gone.bin").unlink()
        opener, _hold = self.hold_for(missing)
        with self.assertRaisesRegex(proof.ProofError, "missing referenced media"):
            proof.run_online_backup(self.media, self.root / "missing", opener, allow_platform_rename=True)
        self.assert_unpublished("missing")

        corrupt = self.document_for({"uploads/c.bin": b"correct"})
        (self.media / "uploads" / "c.bin").write_bytes(b"tampered")
        opener, _hold = self.hold_for(corrupt)
        with self.assertRaisesRegex(proof.ProofError, "does not match the snapshot"):
            proof.run_online_backup(self.media, self.root / "corrupt", opener, allow_platform_rename=True)
        self.assert_unpublished("corrupt")

        changed = self.document_for({"uploads/d.bin": b"stable"})

        def replace_during_copy():
            (self.media / "uploads" / "d.bin").write_bytes(b"changed-after-pin")

        opener, _hold = self.hold_for(changed)
        with self.assertRaisesRegex(backup.BackupError, "changed during backup"):
            proof.run_online_backup(
                self.media, self.root / "changed", opener, before_copy=replace_during_copy,
                allow_platform_rename=True,
            )
        self.assert_unpublished("changed")

        interrupted = self.document_for({"uploads/e.bin": b"copy"})

        def interrupt():
            raise proof.ProofError("backup interrupted")

        opener, _hold = self.hold_for(interrupted)
        with self.assertRaisesRegex(proof.ProofError, "backup interrupted"):
            proof.run_online_backup(self.media, self.root / "interrupt", opener, before_copy=interrupt)
        self.assert_unpublished("interrupt")

        full = self.document_for({"uploads/f.bin": b"disk"})

        def no_space(_source, _target, _signature):
            raise OSError(28, "No space left on device")

        opener, _hold = self.hold_for(full)
        with self.assertRaises(OSError):
            proof.run_online_backup(self.media, self.root / "full", opener, copy_impl=no_space)
        self.assert_unpublished("full")

    def test_variant_replaced_before_observation_is_not_caught_by_size(self):
        key = "derivatives/thumb-320/aaa.jpg"
        proof.link_immutable(self.media, key, b"NEW!")
        document = {
            "posts": [],
            "variants": [{
                "post_id": 1, "object_key": key, "url": "/media/" + key,
                "file_size": 4, "status": "ready",
            }],
            "job_keys": [],
        }
        result, _hold = self.backup(document, name="variant-size")
        stored = (result.output / "media" / "derivatives" / "thumb-320" / "aaa.jpg").read_bytes()
        self.assertEqual(stored, b"NEW!")
        self.assertEqual(len(stored), 4)

    def test_busy_and_s3_and_non_atomic_platform_publish_nothing(self):
        document = self.document_for({"uploads/a.bin": b"a"})

        class Busy(MemoryHold):
            def __init__(self):
                super().__init__(document)
                self.busy = True

        busy = Busy()
        with self.assertRaises(proof.ProofBusy):
            proof.run_online_backup(self.media, self.root / "busy", lambda _protocol: busy)
        self.assertFalse(busy.dumped)
        self.assertTrue(busy.closed)
        self.assert_unpublished("busy")

        def refuse(_protocol):
            raise AssertionError("snapshot must not open when object storage is configured")

        with mock.patch.dict(os.environ, {"S3_BUCKET": "example"}):
            with self.assertRaisesRegex(backup.BackupError, "S3"):
                proof.run_online_backup(self.media, self.root / "s3", refuse)
        self.assert_unpublished("s3")

        def deny_rename(_source, _destination):
            raise backup.BackupError("This platform lacks an atomic no-overwrite directory rename; refusing publication")

        opener, _hold = self.hold_for(document)
        with mock.patch.object(backup, "exclusive_rename", side_effect=deny_rename):
            with self.assertRaisesRegex(proof.ProofError, "atomic publication is unavailable"):
                proof.run_online_backup(self.media, self.root / "nonatomic", opener)
        self.assert_unpublished("nonatomic")

    def test_exclusive_rename_is_the_publication_path(self):
        document = self.document_for({"uploads/a.bin": b"a"})
        opener, _hold = self.hold_for(document)

        def rename(source, destination):
            os.rename(source, destination)

        with mock.patch.object(backup, "exclusive_rename", side_effect=rename) as exclusive:
            result = proof.run_online_backup(self.media, self.root / "exclusive", opener)
        exclusive.assert_called_once()
        self.assertEqual(result.publication, "exclusive-rename")
        backup.verify_backup(result.output)


if __name__ == "__main__":
    unittest.main()
