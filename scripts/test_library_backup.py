"""Isolated tests for library-backup.py; never connects to a real database."""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("library-backup.py")
SPEC = importlib.util.spec_from_file_location("library_backup", SCRIPT)
backup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(backup)


class LibraryBackupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()

    def write_backup(self, root, files=None):
        root.mkdir(mode=0o700)
        entries = []
        values = {"database.dump": b"fake postgres custom dump"}
        values.update(files or {})
        for rel, data in values.items():
            path = root / rel
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            path.write_bytes(data)
            os.chmod(path, 0o600)
            entries.append({"path": rel, "size": len(data), "sha256": hashlib.sha256(data).hexdigest()})
        (root / "manifest.json").write_text(json.dumps({
            "version": 1,
            "created_at": "2026-10-13T00:00:00+00:00",
            "files": entries,
        }), encoding="utf-8")
        return root

    def test_manifest_verifies_and_roundtrip_copy_preserves_bytes(self):
        src = self.root / "source" / "nested" / "image.bin"
        src.parent.mkdir(parents=True)
        payload = bytes(range(256)) * 100
        src.write_bytes(payload)
        backup_dir = self.root / "backup"
        self.write_backup(backup_dir, {"media/nested/image.bin": payload})

        verified = backup.verify_backup(backup_dir)
        restore = self.root / "restored" / "nested" / "image.bin"
        restore.parent.mkdir(parents=True)
        self.assertEqual(backup.copy_and_hash(verified / "media/nested/image.bin", restore),
                         (len(payload), hashlib.sha256(payload).hexdigest()))
        self.assertEqual(restore.read_bytes(), payload)
        self.assertEqual(restore.stat().st_mode & 0o777, 0o600)

    def test_verify_rejects_tampering_extra_file_and_traversal(self):
        root = self.write_backup(self.root / "tampered", {"media/a.jpg": b"one"})
        (root / "media/a.jpg").write_bytes(b"two")
        with self.assertRaisesRegex(backup.BackupError, "integrity"):
            backup.verify_backup(root)

        (root / "media/a.jpg").write_bytes(b"one")
        (root / "unexpected").write_bytes(b"extra")
        with self.assertRaisesRegex(backup.BackupError, "extra"):
            backup.verify_backup(root)
        (root / "unexpected").unlink()

        manifest = json.loads((root / "manifest.json").read_text())
        manifest["files"].append({"path": "../outside", "size": 0, "sha256": hashlib.sha256(b"").hexdigest()})
        (root / "manifest.json").write_text(json.dumps(manifest))
        with self.assertRaisesRegex(backup.BackupError, "unsafe path"):
            backup.verify_backup(root)

    def test_verify_rejects_symlink(self):
        root = self.write_backup(self.root / "linked", {"media/a.jpg": b"a"})
        (root / "media/link").symlink_to(root / "media/a.jpg")
        with self.assertRaisesRegex(backup.BackupError, "only real directories"):
            backup.verify_backup(root)

    def test_create_refuses_existing_destination_before_database_access(self):
        media = self.root / "media"
        media.mkdir()
        target = self.root / "already-there"
        target.mkdir()
        args = type("Args", (), {"offline": True, "media_root": str(media), "output": str(target)})()
        with mock.patch.dict(os.environ, {"DATABASE_URL": "postgresql://user:secret@localhost/kura"}), \
             mock.patch.object(backup, "run_pg") as run_pg:
            with self.assertRaisesRegex(backup.BackupError, "already exists"):
                backup.create_backup(args)
            run_pg.assert_not_called()

    def test_create_failure_cleans_private_staging_without_leaking_credentials(self):
        media = self.root / "media"
        media.mkdir()
        (media / "photo").write_bytes(b"image")
        target = self.root / "backup"
        args = type("Args", (), {"offline": True, "media_root": str(media), "output": str(target)})()

        def fake_run(command, env, label, capture=False):
            if label == "psql source check":
                sql = command[-1]
                if "json_agg" in sql:
                    return "[]"
                if "to_regclass" in sql:
                    return "0"
                self.assertNotIn("secret", " ".join(command))
                self.assertEqual(env["PGPASSWORD"], "secret")
                return "2"
            if label == "pg_dump":
                Path(command[command.index("--file") + 1]).write_bytes(b"dump")
                raise backup.BackupError("synthetic dump failure")
            return ""

        with mock.patch.dict(os.environ, {"DATABASE_URL": "postgresql://user:secret@localhost/kura"}), \
             mock.patch.object(backup, "run_pg", side_effect=fake_run):
            # The core check passes, then dump fails.
            with mock.patch.object(backup, "check_core_database"):
                with self.assertRaisesRegex(backup.BackupError, "synthetic"):
                    backup.create_backup(args)
        self.assertFalse(target.exists())
        self.assertEqual(list(self.root.glob(".kura-backup-*")), [])

    def test_create_copies_media_and_does_not_store_database_credentials(self):
        media = self.root / "media"
        media.mkdir()
        payload = b"original media bytes\x00\xff"
        (media / "photo.bin").write_bytes(payload)
        target = self.root / "published-backup"
        args = type("Args", (), {"offline": True, "media_root": str(media), "output": str(target)})()

        def fake_run(command, env, label, capture=False):
            self.assertNotIn("secret", " ".join(command))
            self.assertNotIn("postgresql://", " ".join(command))
            if label == "psql source check":
                sql = command[-1]
                if "json_agg" in sql:
                    return "[]"
                if "to_regclass" in sql:
                    return "0"
                self.assertEqual(env["PGPASSWORD"], "secret")
                return "2"
            if label == "pg_dump":
                Path(command[command.index("--file") + 1]).write_bytes(b"synthetic dump")
            return ""

        with mock.patch.dict(os.environ, {"DATABASE_URL": "postgresql://user:secret@localhost/kura"}), \
             mock.patch.object(backup, "run_pg", side_effect=fake_run):
            backup.create_backup(args)
        backup.verify_backup(target)
        self.assertEqual((target / "media/photo.bin").read_bytes(), payload)
        self.assertEqual(target.stat().st_mode & 0o777, 0o700)
        self.assertEqual((target / "database.dump").stat().st_mode & 0o777, 0o600)
        manifest_text = (target / "manifest.json").read_text(encoding="utf-8")
        self.assertNotIn("user", manifest_text)
        self.assertNotIn("secret", manifest_text)
        self.assertNotIn("localhost", manifest_text)

    def test_restore_refuses_nonempty_target_before_restore_command(self):
        backup_dir = self.write_backup(self.root / "valid", {"media/a.jpg": b"img"})
        args = type("Args", (), {"offline": True, "input": str(backup_dir), "media_root": str(self.root / "out")})()
        calls = []

        def fake_run(command, env, label, capture=False):
            calls.append(label)
            if label == "pg_restore archive check":
                return ""
            if label == "psql target check":
                return "0|1|0|0|0"
            self.fail("restore should not be invoked for a nonempty database")

        with mock.patch.dict(os.environ, {"DATABASE_URL": "postgresql://user:secret@localhost/kura"}), \
             mock.patch.object(backup, "run_pg", side_effect=fake_run):
            with self.assertRaisesRegex(backup.BackupError, "no user schemas"):
                backup.restore_backup(args)
        self.assertEqual(calls, ["pg_restore archive check", "psql target check"])
        self.assertFalse((self.root / "out").exists())

    def test_restore_uses_one_private_staged_dump_and_publishes_only_media(self):
        backup_dir = self.write_backup(self.root / "valid", {"media/uploads/a.jpg": b"photo"})
        destination = self.root / "restored-media"
        args = type("Args", (), {"offline": True, "input": str(backup_dir), "media_root": str(destination)})()
        dump_paths = []

        def fake_run(command, env, label, capture=False):
            if label == "pg_restore archive check":
                dump_paths.append(Path(command[-1]))
                return ""
            if label == "psql target check":
                return "0|0|0|0|0"
            if label == "pg_restore":
                dump_paths.append(Path(command[-1]))
                return ""
            self.fail(f"unexpected PostgreSQL command: {label}")

        with mock.patch.dict(os.environ, {"DATABASE_URL": "postgresql://user:secret@localhost/kura"}), \
             mock.patch.object(backup, "run_pg", side_effect=fake_run):
            backup.restore_backup(args)
        self.assertEqual(dump_paths[0], dump_paths[1])
        self.assertNotEqual(dump_paths[0], backup_dir / "database.dump")
        self.assertEqual((destination / "uploads/a.jpg").read_bytes(), b"photo")
        self.assertFalse((destination / "database.dump").exists())

    def test_restore_interrupt_retains_private_media_and_reports_recovery_path(self):
        backup_dir = self.write_backup(self.root / "valid", {"media/a.jpg": b"photo"})
        destination = self.root / "restored-media"
        args = type("Args", (), {"offline": True, "input": str(backup_dir), "media_root": str(destination)})()

        def fake_run(command, env, label, capture=False):
            if label == "pg_restore archive check":
                return ""
            if label == "psql target check":
                return "0|0|0|0|0"
            if label == "pg_restore":
                raise KeyboardInterrupt()
            self.fail(f"unexpected PostgreSQL command: {label}")

        with mock.patch.dict(os.environ, {"DATABASE_URL": "postgresql://user:secret@localhost/kura"}), \
             mock.patch.object(backup, "run_pg", side_effect=fake_run):
            with self.assertRaisesRegex(backup.BackupError, "outcome may be unknown") as raised:
                backup.restore_backup(args)
        message = str(raised.exception)
        retained = Path(message.rsplit(" at ", 1)[1])
        self.assertTrue((retained / "a.jpg").is_file())
        self.assertFalse(destination.exists())

    def test_no_overwrite_directory_publication(self):
        stage = self.root / "stage"
        stage.mkdir()
        destination = self.root / "destination"
        destination.mkdir()
        with self.assertRaises(FileExistsError):
            backup.exclusive_rename(stage, destination)
        self.assertTrue(stage.is_dir())
        self.assertTrue(destination.is_dir())

    def test_database_url_credentials_are_only_in_environment(self):
        database, env = backup.pg_environment("postgresql://myuser:p%40ss@127.0.0.1:5432/kura_db?sslmode=require")
        self.assertEqual(database, "kura_db")
        self.assertEqual(env["PGPASSWORD"], "p@ss")
        self.assertEqual(env["PGUSER"], "myuser")
        self.assertEqual(env["PGSSLMODE"], "require")
        self.assertNotIn("DATABASE_URL", env)
        self.assertNotIn("PGPASSWORD", {k: v for k, v in env.items() if k.startswith("PG") and k != "PGPASSWORD"})

    def test_source_reference_inventory_rejects_missing_remote_and_unsafe_objects(self):
        inventory = {"uploads/original.jpg": (), "derivatives/thumb.jpg": ()}
        backup.check_referenced_media(["/media/uploads/original.jpg", "derivatives/thumb.jpg"], inventory)
        with self.assertRaisesRegex(backup.BackupError, "missing"):
            backup.check_referenced_media(["/media/uploads/missing.jpg"], inventory)
        with self.assertRaisesRegex(backup.BackupError, "remote media"):
            backup.check_referenced_media(["https://cdn.example/image.jpg"], inventory)
        with self.assertRaisesRegex(backup.BackupError, "unsafe"):
            backup.check_referenced_media(["/media/../outside.jpg"], inventory)

    def test_restore_empty_guard_covers_pg_prefixed_schemas_and_user_objects(self):
        captured = []

        def fake_run(command, env, label, capture=False):
            captured.append(command[-1])
            return "0|0|0|0|0"

        with mock.patch.object(backup, "run_pg", side_effect=fake_run):
            backup.assert_target_empty({})
        sql = captured[0]
        self.assertIn("nspname NOT IN ('pg_catalog','information_schema','pg_toast','public')", sql)
        self.assertIn("nspname !~ '^pg_(temp|toast_temp)_[0-9]+$'", sql)
        self.assertNotIn("NOT LIKE 'pg_%'", sql)
        self.assertIn("FROM pg_type", sql)
        self.assertNotIn("typtype IN", sql)
        for catalog in ("pg_operator", "pg_collation", "pg_conversion", "pg_ts_parser",
                        "pg_ts_template", "pg_ts_dict", "pg_ts_config", "pg_event_trigger",
                        "pg_largeobject_metadata", "pg_language", "pg_default_acl",
                        "pg_publication", "pg_subscription"):
            self.assertIn(catalog, sql)


if __name__ == "__main__":
    unittest.main()
