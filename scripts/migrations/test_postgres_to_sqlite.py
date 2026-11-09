import io
import json
import os
from pathlib import Path
import sqlite3
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch
from postgres_to_sqlite import TABLES, convert, export_snapshot, import_snapshot


class PostgresExportTests(unittest.TestCase):
    def test_connects_to_explicit_source_without_credentials_in_arguments(self):
        source = "postgresql://viewer%40example:p%3Ass%2Fword%25@database.internal:6543/reader%20state?sslmode=require&connect_timeout=20&channel_binding=prefer&target_session_attrs=read-only"
        with patch.dict(os.environ, {
            "SOURCE_DATABASE_URL": source, "PGHOST": "wrong.internal",
            "PGHOSTADDR": "127.0.0.1", "PGSERVICE": "wrong-service",
            "PGOPTIONS": "-c default_transaction_read_only=off", "PGPASSWORD": "wrong-password",
        }), patch("postgres_to_sqlite.subprocess.run", return_value=SimpleNamespace(returncode=0)) as run:
            output = io.BytesIO()
            export_snapshot(output)
        args, options = run.call_args
        self.assertEqual(args[0], ["psql", "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1"])
        env = options["env"]
        self.assertEqual({key: env[key] for key in ("PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE")}, {
            "PGHOST": "database.internal", "PGPORT": "6543", "PGUSER": "viewer@example",
            "PGPASSWORD": "p:ss/word%", "PGDATABASE": "reader state",
        })
        self.assertEqual(env["PGSSLMODE"], "require")
        self.assertEqual(env["PGCONNECT_TIMEOUT"], "20")
        self.assertEqual(env["PGCHANNELBINDING"], "prefer")
        self.assertEqual(env["PGTARGETSESSIONATTRS"], "read-only")
        for key in ("SOURCE_DATABASE_URL", "PGHOSTADDR", "PGSERVICE", "PGOPTIONS"):
            self.assertNotIn(key, env)
        self.assertIs(options["stdout"], output)
        sql = options["input"].decode()
        self.assertTrue(sql.startswith("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;"))
        self.assertTrue(sql.endswith("COMMIT;"))
        for table in TABLES:
            self.assertIn(f"FROM public.{table} t", sql)
        self.assertIn("WHERE EXISTS (SELECT 1 FROM public.appview_publication_scopes", sql)

    def test_supports_ipv6_and_default_port_without_using_local_socket(self):
        for source, host in [("postgres://user:secret@[2001:db8::1]/reader", "2001:db8::1"),
                             ("postgresql://user:secret@database.internal/reader", "database.internal")]:
            with self.subTest(source=host), patch.dict(os.environ, {"SOURCE_DATABASE_URL": source}), patch("postgres_to_sqlite.subprocess.run", return_value=SimpleNamespace(returncode=0)) as run:
                export_snapshot(io.BytesIO())
                env = run.call_args.kwargs["env"]
                self.assertEqual(env["PGHOST"], host)
                self.assertEqual(env["PGPORT"], "5432")
                self.assertEqual(env["PGCONNECT_TIMEOUT"], "10")

    def test_queryless_source_does_not_parse_an_empty_query_on_older_python(self):
        # Python 3.10 rejects parse_qsl('', strict_parsing=True).
        with patch.dict(os.environ, {"SOURCE_DATABASE_URL": "postgresql://user:secret@database.internal/reader"}), patch("postgres_to_sqlite.parse_qsl", side_effect=ValueError("bad query field: ''")) as parse, patch("postgres_to_sqlite.subprocess.run", return_value=SimpleNamespace(returncode=0)) as run:
            export_snapshot(io.BytesIO())
            parse.assert_not_called()
            self.assertEqual(run.call_args.kwargs["env"]["PGDATABASE"], "reader")

    def test_rejects_ambiguous_or_unsupported_settings_before_psql(self):
        for source in [
            "", "https://user:secret@database.internal/reader", "postgresql:///reader",
            "postgresql://user:secret@database.internal", "postgresql://user@database.internal/reader",
            "postgresql://user:secret@database.internal:0/reader",
            "postgresql://user:secret@database.internal/reader#fragment",
            "postgresql://user:secret@database.internal/reader?host=other.internal",
            "postgresql://user:secret@database.internal/reader?sslmode=invalid",
            "postgresql://user:secret@database.internal/reader?sslmode=require&sslmode=disable",
            "postgresql://user:secret@database.internal/reader?connect_timeout=0",
            "postgresql://user:secret@database.internal/reader?connect_timeout=601",
            "postgresql://user:secret@database.internal/reader?options=-c%20default_transaction_read_only%3Doff",
        ]:
            with self.subTest(source=source), patch.dict(os.environ, {"SOURCE_DATABASE_URL": source}), patch("postgres_to_sqlite.subprocess.run") as run:
                with self.assertRaises(ValueError) as error:
                    export_snapshot(io.BytesIO())
                self.assertNotIn("secret", str(error.exception))
                run.assert_not_called()

    def test_sanitizes_export_errors_without_echoing_source_or_driver_stderr(self):
        source = "postgresql://user:private-password@database.internal/reader"
        with patch.dict(os.environ, {"SOURCE_DATABASE_URL": source}), patch("postgres_to_sqlite.subprocess.run", return_value=SimpleNamespace(returncode=2, stderr=source.encode())):
            with self.assertRaisesRegex(ValueError, "Postgres export failed") as error:
                export_snapshot(io.BytesIO())
        self.assertNotIn("private-password", str(error.exception))
        self.assertNotIn(source, str(error.exception))


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
