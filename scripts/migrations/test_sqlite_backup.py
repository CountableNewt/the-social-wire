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
            with sqlite3.connect(source) as db:
                db.execute("PRAGMA journal_mode=WAL")
                db.execute("CREATE TABLE private_drafts(value BLOB)")
                db.execute("INSERT INTO private_drafts VALUES (?)", (bytes([0, 1, 255]),))
                db.commit()
                env = dict(os.environ, SQLITE_DB_PATH=str(source))
                subprocess.run([str(tool), str(target)], env=env, check=True, capture_output=True)
                with sqlite3.connect(target) as backup:
                    self.assertEqual(backup.execute("SELECT value FROM private_drafts").fetchone()[0], bytes([0, 1, 255]))
                self.assertEqual(target.stat().st_mode & 0o777, 0o600)
                retry = subprocess.run([str(tool), str(target)], env=env, capture_output=True)
                self.assertNotEqual(retry.returncode, 0)
                self.assertIn(b"already exists", retry.stderr)
