# Database Schema

The standalone Go backend owns SQLite schema migrations in `services/appview/internal`. Each store applies its schema before startup. The retired Postgres migrations and Database Migrator service were removed in #963.

User-authored protocol records remain authoritative on the PDS. SQLite contains derived projections and private unpublished data. Existing hosted Postgres databases and backups are retained separately; source cleanup does not delete hosted resources.

The Go backup command lives at `services/appview/cmd/sqlite-backup`; its snapshot and verification code lives in `internal/sqlitedb`. The old Postgres import scripts and Python runtime dependency are retired. Historical import revisions remain in Git.

Timestamp portability uses stored integer microseconds for content timestamps, read floors, unread overrides and PDS boundaries. Original timestamp strings remain authoritative for API/PDS serialization, and existing feed/cursor ordering retains its nanosecond precision. Startup checks the resulting schema for residual private timestamp dependencies. The source migration and its tests require verification before the Turso cutover.

The opt-in remote backend targets the newer Turso Database engine with the pure-Go serverless driver. AppView and scheduling remain separate databases. Remote serving only verifies readiness; `AppView --initialize-only` owns explicit initialization. Use `database-preflight` on verified offline snapshots before import. Hosted database creation/import and engine compatibility checks are not yet performed.
