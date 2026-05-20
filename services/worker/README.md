# Social Wire — Thin AppView Worker

Jetstream firehose ingestion and TTL cleanup for the GDPR-safe Level-1 read index (`content_items`, `read_marks`).

Shared persistence and indexing live in **`packages/swift/ThinAppViewCore`**. The HTTP API in **`services/api`** reads from the same stores but does not run ingestion.

## Run locally

```bash
cd services/worker
cp .env.example .env
APP_ENV=local ENABLE_THIN_APPVIEW=true swift run Worker
```

## Deploy

From repo root (Docker build context is the monorepo):

```bash
bash scripts/fly-deploy-worker.sh dev   # or main
```

Fly configs: `fly.toml` (dev), `fly.prod.toml` (prod).

## Environment

Same database variables as the API (`APP_ENV`, `SUPABASE_DATABASE_URL`, `SQLITE_DB_PATH`) plus Thin AppView flags (`ENABLE_THIN_APPVIEW`, relay URL, TTLs). See `.env.example`.
