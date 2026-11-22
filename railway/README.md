# Historical Railway Configuration

Railway deployment configurations were retired from source in issue #963. Current hosting uses Vercel Web and AppView projects with Turso databases and private Blob storage. See the [current runtime guide](../docs/architecture/standalone-reader.md).

Historical configuration remains available in Git. Removing source files does not delete hosted services, volumes, buckets or backups; operator retirement remains a separate step after public cutover acceptance. Keep the old writer frozen until that step completes. Local Docker images, SQLite, backup and preflight commands remain supported.
