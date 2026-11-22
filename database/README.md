# Database Schema

The Go backend owns schema migrations in `services/appview/internal`. Hosted AppView and scheduling use distinct US Turso databases for Development and Production. Local development and recovery retain SQLite. The retired Postgres migrations and Database Migrator service were removed in #963.

User-authored protocol records remain authoritative on the PDS. Application stores contain derived projections and private unpublished data. Private attachment ciphertext lives in private Blob storage; preserve its existing encryption key with the database recovery set.

Local SQLite stores apply migrations before startup. Remote serving verifies schema readiness and encrypted-vault compatibility without startup DDL. Explicit `AppView --initialize-only` initializes both remote stores before serving. Development uses the `dev` branch Preview in the same retained Vercel Services project as Production (source target; #992 tracks hosted acceptance); initialize its databases explicitly outside builds. Preview builds never initialize shared release databases.

The Go `services/appview/cmd/sqlite-backup` command creates verified local SQLite snapshots, including committed WAL pages, with no-overwrite and private-file protections. Run `database-preflight` on verified offline snapshots before import. Retired Postgres import scripts and the Python runtime dependency remain historical Git references. Turso exports have their own snapshot/log recovery procedure; do not treat a main file with unapplied log data as a complete SQLite backup.

Timestamp portability uses stored integer microseconds for content timestamps, read floors, unread overrides and PDS boundaries. Original timestamp strings remain authoritative for API/PDS serialization, and feed/cursor ordering retains nanosecond precision. Schema checks reject residual private timestamp extensions.

Hosted cutover and resource retirement evidence is tracked in issue #963. Keep any retired Railway writer frozen until its infrastructure is removed; deleting source configuration does not delete hosted data or backups.
