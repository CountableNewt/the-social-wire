# Railway Deployment Configuration

Development and Production currently run the Next.js Web app and standalone Go AppView on Railway. GitHub Actions validates source; Railway deploys linked revisions after required checks. Development tracks `dev`; Production tracks `main`.

| Service | Config File |
| --- | --- |
| Web | `/railway/web-standalone.json` |
| AppView (current local SQLite target) | `/railway/appview-standalone.json` |
| AppView (opt-in Turso target, pending acceptance) | `/railway/appview-turso.json` |

Both images build from the monorepo root. AppView retains the existing API domains, requires a persistent `/data` volume, uses one replica in `sfo`, and disables deployment overlap and sleeping. The image entrypoint owns startup; do not override it with a legacy service command. SQLite migrations currently run before the listener starts.

The Swift Gateway, distributed AppView, indexing workers, Postgres, and Database Migrator source/configuration have been retired as part of #963. Historical revisions remain in Git. Removing their hosted services, volumes, variables, or backups is a separate operator action; deleting source does not change Railway resources.

Follow the Development-first data and scheduling cutover in [the standalone reader guide](../docs/architecture/standalone-reader.md) and [the scheduling runbook](../docs/runbooks/embedded-scheduling-cutover.md). Preserve the existing scheduling encryption key when renaming its variable. The Go `sqlite-backup` command replaces the retired Python backup/import tooling.

Vercel and Turso migration remains tracked in #963. Keep Railway running until the hosted cutover and acceptance requirements in that issue are met.

The opt-in Turso config requires both distinct database endpoint/token pairs and `DATABASE_BACKEND=turso`. It runs explicit schema initialization before deployment and does not require a local volume. Do not select it until import, backup/restore, engine compatibility and Development acceptance are ready.
