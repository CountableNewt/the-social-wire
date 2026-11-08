import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from postgres_to_sqlite import TABLES, convert, import_snapshot


class SQLiteCutoverTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.path = Path(self.directory.name) / "target.sqlite"
        with sqlite3.connect(self.path) as db:
            db.executescript("""
                CREATE TABLE appview_pds_projection_write(singleton INTEGER PRIMARY KEY, active INTEGER NOT NULL);
                INSERT INTO appview_pds_projection_write VALUES(1,0);
                CREATE TABLE appview_pds_read_state_authority(viewer_did TEXT PRIMARY KEY, manifest TEXT,
                    manifest_cid TEXT, legacy_revision INTEGER, projection_ready INTEGER, last_accessed_at REAL);
                CREATE TABLE private_social_draft_accounts(owner TEXT PRIMARY KEY);
                CREATE TABLE private_social_draft_assets(owner TEXT REFERENCES private_social_draft_accounts(owner),
                    asset_id TEXT PRIMARY KEY, metadata BLOB, created_at REAL);
                CREATE TABLE private_article_drafts(owner TEXT, draft_id TEXT PRIMARY KEY, payload BLOB,
                    mutation_hash BLOB, deleted INTEGER, updated_at REAL);
            """)

    def tearDown(self):
        self.directory.cleanup()

    def snapshot(self, rows=None):
        rows = rows or {}
        records = []
        for table in TABLES:
            records.append({"table": table, "count": len(rows.get(table, []))})
            records.extend({"table": table, "row": row} for row in rows.get(table, []))
        return io.StringIO("\n".join(json.dumps(record) for record in records))

    def test_preserves_encrypted_bytes_and_fences_pds_recovery(self):
        rows = {
            "appview_pds_read_state_authority": [{"viewer_did": "did:plc:owner", "manifest": {"version": 2},
                "manifest_cid": "bafy", "legacy_revision": 4, "projection_ready": True,
                "last_accessed_at": "2026-11-07T10:00:00.123456+00:00"}],
            "private_social_draft_accounts": [{"owner": "did:plc:owner"}],
            "private_social_draft_assets": [{"owner": "did:plc:owner", "asset_id": "asset", "metadata": "\\x0001ff",
                "created_at": "2026-11-07T10:00:00.123456+00:00"}],
            "private_article_drafts": [{"owner": "did:plc:owner", "draft_id": "draft", "payload": "\\x010203",
                "mutation_hash": "\\x040506", "deleted": False, "updated_at": "2026-11-07T10:00:00+00:00"}],
        }
        counts = import_snapshot(self.path, self.snapshot(rows))
        self.assertEqual(counts["private_article_drafts"], 1)
        with sqlite3.connect(self.path) as db:
            self.assertEqual(db.execute("SELECT metadata,typeof(created_at) FROM private_social_draft_assets").fetchone(), (bytes([0, 1, 255]), "real"))
            self.assertEqual(db.execute("SELECT payload,mutation_hash,deleted FROM private_article_drafts").fetchone(), (bytes([1, 2, 3]), bytes([4, 5, 6]), 0))
            self.assertEqual(db.execute("SELECT manifest_cid,projection_ready,legacy_revision FROM appview_pds_read_state_authority").fetchone(), ("bafy", 0, 4))
            self.assertEqual(db.execute("SELECT active FROM appview_pds_projection_write").fetchone()[0], 0)
        with self.assertRaisesRegex(ValueError, "not empty"):
            import_snapshot(self.path, self.snapshot(rows))

    def test_incomplete_snapshot_rolls_back_every_table(self):
        stream = self.snapshot({"private_social_draft_accounts": [{"owner": "owner"}]})
        truncated = io.StringIO("\n".join(stream.getvalue().splitlines()[:-1]))
        with self.assertRaisesRegex(ValueError, "incomplete"):
            import_snapshot(self.path, truncated)
        with sqlite3.connect(self.path) as db:
            self.assertEqual(db.execute("SELECT count(*) FROM private_social_draft_accounts").fetchone()[0], 0)
            self.assertEqual(db.execute("SELECT active FROM appview_pds_projection_write").fetchone()[0], 0)

    def test_missing_feature_schema_prevents_data_loss(self):
        with self.assertRaisesRegex(ValueError, "Enable the retained feature"):
            import_snapshot(self.path, self.snapshot({"creator_analytics_items": [{"subject_uri": "subject"}]}))

    def test_conversion_preserves_microseconds_json_and_rejects_ambiguous_dates(self):
        self.assertEqual(convert("created_at", "TEXT", "2026-11-07T04:00:00.123456-06:00"), "2026-11-07T10:00:00.123456Z")
        self.assertEqual(convert("scope_keys", "TEXT", ["a", "b"]), '["a","b"]')
        self.assertEqual(convert("lease_until", "REAL", "1970-01-01T00:02:00Z"), 120.0)
        with self.assertRaisesRegex(ValueError, "timezone"):
            convert("created_at", "REAL", "2026-11-07T10:00:00")
        with self.assertRaisesRegex(ValueError, "Invalid source timestamp"):
            convert("created_at", "REAL", "private invalid input")


if __name__ == "__main__":
    unittest.main()
