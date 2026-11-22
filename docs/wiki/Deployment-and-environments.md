# Deployment and Environments

Development tracks `dev`; Production tracks `main`. Required CI enforces Development-first patch parity. Railway currently builds the protected branches through its GitHub integration; GitHub Actions does not deploy or mutate hosted databases.

| Environment | Web | Go AppView |
|-------------|-----|------------|
| Development | `testing.thesocialwire.app` | `api.testing.thesocialwire.app` |
| Production | `thesocialwire.app` | `api.thesocialwire.app` |

Use `railway/web-standalone.json` and `railway/appview-standalone.json`. The Go backend uses one US West replica with a persistent `/data` volume holding AppView and scheduling SQLite databases. The retired distributed Swift services, Operations Web, Postgres migrator, Redis package and infrastructure graph are removed from source.

Hosted service, database, volume and bucket deletion remains a separate environment-scoped operator action. Preserve backups and encryption keys; source removal does not delete them.

Issue #963 tracks the remaining database portability, durable coordination, Turso and Vercel steps. Do not describe those future targets as deployed. Preserve OAuth client IDs, redirect hosts and existing API domains during the eventual cutover.

See the [standalone backend runbook](https://github.com/CountableNewt/the-social-wire/blob/main/docs/architecture/standalone-reader.md) for configuration and recovery.
