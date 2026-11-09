from contextlib import closing
import importlib.util
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest


class SQLiteBackupTests(unittest.TestCase):
    def test_backup_includes_wal_and_handles_quoted_paths_without_overwrite(self):
        tool = Path(__file__).resolve().parents[2] / "services/appview/bin/sqlite-backup"
        with tempfile.TemporaryDirectory(prefix="tsw-backup-") as root:
            source = Path(root) / "source.sqlite"
            target = Path(root) / "backup's copy.sqlite"
            with closing(sqlite3.connect(source)) as db, db:
                db.execute("PRAGMA journal_mode=WAL")
                db.execute("CREATE TABLE private_drafts(value BLOB)")
                db.execute("INSERT INTO private_drafts VALUES (?)", (bytes([0, 1, 255]),))
                db.commit()
                env = dict(os.environ, SQLITE_DB_PATH=str(source))
                subprocess.run([str(tool), str(target)], env=env, check=True, capture_output=True)
                with closing(sqlite3.connect(target)) as backup:
                    self.assertEqual(backup.execute("SELECT value FROM private_drafts").fetchone()[0], bytes([0, 1, 255]))
                self.assertEqual(target.stat().st_mode & 0o777, 0o600)
                retry = subprocess.run([str(tool), str(target)], env=env, capture_output=True)
                self.assertNotEqual(retry.returncode, 0)
                self.assertIn(b"already exists", retry.stderr)

    def test_backup_preserves_custom_timestamp_index_and_wal(self):
        tool = Path(__file__).resolve().parents[2] / "services/appview/bin/sqlite-backup"
        with tempfile.TemporaryDirectory(prefix="tsw-backup-index-") as root:
            source = Path(root) / "source.sqlite"
            target = Path(root) / "backup.sqlite"
            with closing(sqlite3.connect(source)) as db, db:
                # The production index uses chronological comparisons across
                # legacy plain dates and imported microsecond-precision dates.
                module_path = Path(__file__).with_name("postgres_to_sqlite.py")
                spec = importlib.util.spec_from_file_location("backup_test_timestamps", module_path)
                timestamps = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(timestamps)
                compare = lambda a, b: (timestamps.timestamp(a) > timestamps.timestamp(b)) - (timestamps.timestamp(a) < timestamps.timestamp(b))
                db.create_collation("tsw_timestamp", compare)
                db.execute("PRAGMA journal_mode=WAL")
                db.execute("PRAGMA wal_autocheckpoint=0")
                db.execute("CREATE TABLE entries(uri TEXT PRIMARY KEY, created_at TEXT)")
                db.execute("CREATE INDEX entries_timestamp ON entries(created_at COLLATE tsw_timestamp, uri)")
                rows = [("plain", "2026-04-23T09:18:21Z"), ("fractional", "2026-04-23T09:18:21.851234Z")]
                db.executemany("INSERT INTO entries VALUES (?, ?)", rows)
                db.commit()
                self.assertGreater(Path(str(source) + "-wal").stat().st_size, 0)
                # A live writer can keep an uncommitted transaction open: the
                # backup must read the committed snapshot without taking its lock.
                db.execute("INSERT INTO entries VALUES (?, ?)", ("pending", "2026-04-23T09:18:22Z"))
                result = subprocess.run([str(tool), str(target)], env=dict(os.environ, SQLITE_DB_PATH=str(source)), capture_output=True)
                self.assertEqual(result.returncode, 0, result.stderr.decode())
                with closing(sqlite3.connect(target)) as backup:
                    backup.create_collation("tsw_timestamp", compare)
                    self.assertEqual(backup.execute("SELECT uri FROM entries ORDER BY created_at COLLATE tsw_timestamp DESC").fetchall(), [("fractional",), ("plain",)])
                    self.assertEqual(backup.execute("PRAGMA integrity_check").fetchall(), [("ok",)])
                    self.assertEqual(backup.execute("SELECT * FROM entries ORDER BY uri").fetchall(), sorted(rows))
                self.assertEqual(target.stat().st_mode & 0o777, 0o600)
                self.assertEqual(db.execute("SELECT count(*) FROM entries").fetchone()[0], 3)
                db.rollback()
                self.assertEqual(db.execute("SELECT count(*) FROM entries").fetchone()[0], 2)

    def test_backup_rejects_symlinks_and_relative_destinations(self):
        tool = Path(__file__).resolve().parents[2] / "services/appview/bin/sqlite-backup"
        with tempfile.TemporaryDirectory(prefix="tsw-backup-paths-") as root:
            source = Path(root) / "source.sqlite"
            with closing(sqlite3.connect(source)) as db, db:
                db.execute("CREATE TABLE entries(value TEXT)")
            source_link = Path(root) / "source-link.sqlite"
            source_link.symlink_to(source)
            target = Path(root) / "backup.sqlite"
            result = subprocess.run([str(tool), str(target)], env=dict(os.environ, SQLITE_DB_PATH=str(source_link)), capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(target.exists())
            target.symlink_to(Path(root) / "missing.sqlite")
            result = subprocess.run([str(tool), str(target)], env=dict(os.environ, SQLITE_DB_PATH=str(source)), capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b"already exists", result.stderr)
            self.assertTrue(target.is_symlink())
            result = subprocess.run([str(tool), "relative.sqlite"], env=dict(os.environ, SQLITE_DB_PATH=str(source)), cwd=root, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((Path(root) / "relative.sqlite").exists())

    def test_corruption_and_foreign_key_failure_leave_no_verified_backup(self):
        tool = Path(__file__).resolve().parents[2] / "services/appview/bin/sqlite-backup"
        with tempfile.TemporaryDirectory(prefix="tsw-backup-invalid-") as root:
            source = Path(root) / "source.sqlite"
            target = Path(root) / "backup.sqlite"
            source.write_bytes(b"invalid sqlite database: private contents")
            result = subprocess.run([str(tool), str(target)], env=dict(os.environ, SQLITE_DB_PATH=str(source)), capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn(b"private contents", result.stderr)
            self.assertFalse(target.exists())
            source.unlink()
            with closing(sqlite3.connect(source)) as db, db:
                db.execute("CREATE TABLE parents(id INTEGER PRIMARY KEY)")
                db.execute("CREATE TABLE children(parent INTEGER REFERENCES parents(id))")
                db.execute("INSERT INTO children VALUES (999)")
            result = subprocess.run([str(tool), str(target)], env=dict(os.environ, SQLITE_DB_PATH=str(source)), capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(target.exists())
            self.assertNotIn(b"999", result.stderr)
