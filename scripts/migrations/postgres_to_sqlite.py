#!/usr/bin/env python3
"""Copy retained state from a read-only Postgres snapshot into an initialized empty SQLite DB.

Stop the old writer services before final export. Source is never mutated. The target
backend must be stopped. Preserve the Postgres database and S3 storage keys for rollback.
"""
import argparse
import collections
import datetime
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile

# Parents precede children. Derived caches, counters, ingestion history and algorithms
# stay in Postgres for rollback; the new backend rebuilds its disposable projections.
TABLES = (
    "appview_publication_scopes", "appview_viewer_feeds", "appview_feed_publications",
    "content_items", "rss_feed_fetch_metadata", "read_marks",
    "appview_unread_overrides", "appview_publication_read_floors",
    "appview_pds_read_state_authority",
    "private_social_draft_accounts", "private_social_draft_assets",
    "private_social_draft_manifests", "private_social_draft_bindings",
    "private_social_draft_cleanup", "private_article_drafts", "private_article_draft_bindings",
    "creator_analytics_items", "creator_analytics_refresh", "creator_analytics_snapshots",
    "creator_analytics_events", "creator_analytics_tracking",
    "podcast_shows", "podcast_episodes", "podcast_aliases", "podcast_subscriptions",
    "podcast_viewer_state", "podcast_jobs", "podcast_clips",
    "podcast_private_shows", "podcast_private_episodes",
)


def predicate(table):
    if table == "content_items":
        return " WHERE EXISTS (SELECT 1 FROM public.appview_publication_scopes s WHERE s.author_did=t.author_did)"
    return ""


def export_snapshot(destination):
    source = os.environ.get("SOURCE_DATABASE_URL")
    if not source:
        raise ValueError("Set SOURCE_DATABASE_URL; database credentials are never accepted in arguments")
    statements = ["BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;", "SET LOCAL statement_timeout='10min';"]
    for table in TABLES:
        where = predicate(table)
        statements.append(f"SELECT json_build_object('table','{table}','count',count(*)) FROM public.{table} t{where};")
        statements.append(f"SELECT json_build_object('table','{table}','row',row_to_json(t)) FROM public.{table} t{where};")
    statements.append("COMMIT;")
    env = dict(os.environ, PGDATABASE=source, PGAPPNAME="social-wire-sqlite-export")
    # Connection strings never appear in argv or diagnostic output.
    result = subprocess.run(["psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1"],
                            input="\n".join(statements).encode(), stdout=destination,
                            stderr=subprocess.PIPE, env=env, check=False)
    if result.returncode:
        raise ValueError("Postgres export failed; check connectivity/schema with psql (source unchanged)")


def timestamp(value):
    try:
        parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except (ValueError, AttributeError):
        raise ValueError("Invalid source timestamp") from None
    if parsed.tzinfo is None:
        raise ValueError("Source timestamp lacks timezone")
    return parsed.astimezone(datetime.timezone.utc)


def convert(column, declared_type, value):
    if value is None:
        return None
    kind = declared_type.upper()
    if kind == "BLOB":
        if not isinstance(value, str) or not value.startswith("\\x"):
            raise ValueError(f"Expected Postgres hex bytea for {column}")
        return bytes.fromhex(value[2:])
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    if isinstance(value, str) and (column.endswith("_at") or column in ("backoff_until", "lease_until")):
        parsed = timestamp(value)
        if kind in ("REAL", "DOUBLE", "FLOAT"):
            return parsed.timestamp()
        return parsed.isoformat(timespec="microseconds").replace("+00:00", "Z")
    return value


def import_snapshot(destination, snapshot):
    if not Path(destination).is_file():
        raise ValueError("Initialize the empty target with AppView --initialize-only first")
    counts = collections.Counter()
    expected = {}
    with sqlite3.connect(f"file:{Path(destination).resolve()}?mode=rw", uri=True) as db:
        db.create_collation("tsw_timestamp", lambda left, right: (timestamp(left) > timestamp(right)) - (timestamp(left) < timestamp(right)))
        db.create_function("tsw_timestamp_us", 1, lambda value: round(timestamp(value).timestamp() * 1_000_000) if value else None, deterministic=True)
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("PRAGMA busy_timeout=10000")
        schema = {}
        for table in TABLES:
            columns = db.execute(f'PRAGMA table_info("{table}")').fetchall()
            schema[table] = {row[1]: row[2] for row in columns}
            if columns and db.execute(f'SELECT EXISTS(SELECT 1 FROM "{table}" LIMIT 1)').fetchone()[0]:
                raise ValueError(f"Target table {table} is not empty; refusing to overwrite data")
        db.execute("BEGIN IMMEDIATE")
        try:
            # Disable legacy export revision/fence triggers only within this transaction.
            db.execute("UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1")
            for line in snapshot:
                record = json.loads(line)
                table = record["table"]
                if table not in TABLES:
                    raise ValueError("Snapshot contains an unexpected table")
                if "count" in record:
                    if table in expected:
                        raise ValueError(f"Duplicate table header: {table}")
                    expected[table] = record["count"]
                    if expected[table] and not schema[table]:
                        raise ValueError(f"Enable the retained feature schema before importing {table}")
                    continue
                if table not in expected:
                    raise ValueError("Snapshot row precedes its table header")
                row = record["row"]
                columns = [name for name in schema[table] if name in row]
                values = [convert(name, schema[table][name], row[name]) for name in columns]
                if table == "appview_pds_read_state_authority":
                    # Rebuild from verified PDS content on first request. Legacy rows must
                    # never become a fallback for an already migrated viewer.
                    if "projection_ready" in columns:
                        values[columns.index("projection_ready")] = 0 if row.get("manifest_cid") else 1
                    else:
                        columns.append("projection_ready")
                        values.append(0 if row.get("manifest_cid") else 1)
                if not columns:
                    raise ValueError(f"No compatible columns in {table}")
                names = ",".join(f'"{name}"' for name in columns)
                placeholders = ",".join("?" for _ in columns)
                db.execute(f'INSERT INTO "{table}" ({names}) VALUES ({placeholders})', values)
                counts[table] += 1
            if set(expected) != set(TABLES) or any(counts[t] != expected[t] for t in TABLES):
                raise ValueError("Snapshot is incomplete or table counts do not match")
            violations = db.execute("PRAGMA foreign_key_check").fetchall()
            if violations:
                raise ValueError("Imported snapshot violates foreign keys")
            if db.execute("PRAGMA quick_check").fetchone()[0] != "ok":
                raise ValueError("Imported database failed integrity verification")
            db.execute("UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1")
            db.commit()
        except BaseException:
            db.rollback()
            raise
    return dict(counts)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", required=True, help="Initialized empty SQLite database; backend must be stopped")
    parser.add_argument("--snapshot", help="Existing private NDJSON snapshot (otherwise export read-only from SOURCE_DATABASE_URL)")
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if args.snapshot:
            with open(args.snapshot, encoding="utf-8") as stream:
                counts = import_snapshot(args.destination, stream)
        else:
            with tempfile.TemporaryFile(mode="w+b") as stream:
                export_snapshot(stream)
                stream.seek(0)
                counts = import_snapshot(args.destination, stream)
        print(json.dumps({"importedRows": counts, "sourceModified": False}, sort_keys=True))
    except (ValueError, sqlite3.Error, OSError) as error:
        # Values and SQL bindings may contain private draft content; print only our own
        # validation messages, never database exceptions or connection strings.
        message = str(error) if isinstance(error, ValueError) else "Migration failed; target transaction rolled back"
        parser.exit(1, message + "\n")


if __name__ == "__main__":
    main()
