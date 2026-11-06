# Indexing Worker

This Swift package hosts two independently replicated Railway service classes:

- `INDEXING_WORKER_ROLE=projection` runs the AppView durable-inbox projector. PostgreSQL claim/ack semantics partition work across replicas.
- `INDEXING_WORKER_ROLE=coordinator` runs AppView RSS, backfill, retention, and repository recovery jobs. A fenced role lease allows one active owner while other replicas remain healthy standbys.

The active AppView lane is supported. Wire, Finance, and Sports feed modes must remain `off`; dormant workers and Operations HTTP services are excluded. A repository recovery that cannot use `listRecords` verifies a signed CAR snapshot, canonical MST, and current DID/PDS/key binding before advancing its fenced checkpoint.

For RSS and retention without global ingestion, set `THIN_APPVIEW_WORKER_MODE=maintenance` and keep `INDEXING_WORKER_ROLE=coordinator`. This mode never builds or claims inbox, replay, proactive-backfill, or repository-reconciliation work, even if stale recovery flags remain set. It needs no Jetstream checkpoint, relay, or PLC configuration. Keep `OPERATIONS_RECOVERY_ENABLED=false` explicit in deployment configuration. Readiness requires an active Coordinator fence, successful and timely retention and enabled RSS polling sweeps, and a live configured RSS cache; completeness remains unknown because this worker does not measure global content coverage. Ordinary per-feed fetch failures remain feed-level metadata, rather than a claim that every publisher is fresh.

In ingestion mode (the default), required hosted variables include `APP_ENV`, `DATABASE_URL`, `ENABLE_THIN_APPVIEW=true`, `THIN_APPVIEW_JETSTREAM_MODE=v2_authoritative`, `INDEXING_WORKER_ROLE`, and existing AppView role settings. Coordinator lease timing defaults to a 30-second lease, 10-second renewal, and 5-second standby retry; override with `INDEXING_ROLE_LEASE_SECONDS`, `INDEXING_ROLE_LEASE_RENEW_SECONDS`, and `INDEXING_ROLE_STANDBY_RETRY_SECONDS` only as a coordinated operational change.

The public health listener uses `PORT`. The AppView component probe binds loopback on `PORT + 1` by default. `/startupz` verifies PostgreSQL and component startup. `/readyz` checks maintenance scheduler progress in maintenance mode and fresh authoritative ingestion evidence in ingestion mode; a coordinator standby reports ready only with fresh lease-control evidence. Authority and diagnostic pools remain separate from workload pools. SIGTERM cancels and joins owned resources before process exit.

Run locally:

```sh
swift test
INDEXING_WORKER_ROLE=projection APP_ENV=dev DATABASE_URL='postgresql://…' ENABLE_THIN_APPVIEW=true THIN_APPVIEW_JETSTREAM_MODE=v2_authoritative swift run IndexingWorker
```

The image builds native Swift `IndexingWorker`, `retry-reconciliation`, and the canonical `schema-ready` migration gate. Shared libraries remain separately versioned package dependencies.

`retry-reconciliation` is an explicit operator action. It requires its own `RECONCILIATION_RETRY_DATABASE_URL` variable and never falls back to `DATABASE_URL`:

```sh
swift run retry-reconciliation --environment dev --generation exact-source-generation --expected-count 2 --apply
```

The command atomically requeues exactly the requested number (1–100) of failed reconciliation requests with matching unreconciled dead-letter evidence. Count mismatches roll back. Historical attempts, recovery checkpoints, terminal events, and ingestion watermarks remain intact. Errors emit only `invalid_scope`, `expected_count_mismatch`, or `retry_failed`.
