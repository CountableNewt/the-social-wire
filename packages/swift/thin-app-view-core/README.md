# ThinAppViewCore

Extracted from The Social Wire at `47368fefb52ad90e37f6d0962a87451b04a04c56`. See `SOURCE_PROVENANCE.json` for the original source digests. Package history is preserved by filtering its original directory; packaging changes follow that history.

Run `swift test` with Swift 6.2.4 or newer. CI enforces warnings as errors without unsafe library flags.

The reviewed migration snapshot under `TestSupport/Migrations` prepares disposable integration databases. Set `DATABASE_URL` and run `bash scripts/apply-test-migrations.sh`; production migration ownership remains in The Social Wire. CI applies the snapshot twice to verify its migration ledger before enabling database tests.

---

# ThinAppViewCore

Shared Swift package for the **Thin AppView** read index — data-minimized standard.site rows, RSS content rows, derived read marks, and Redis/Postgres/SQLite projection cache implementations.

Consumed by:

- **`services/appview`** — `/v1/appview/*`, `/v1/publications/*`, bootstrap stream
- **`services/appview-worker`** — Jetstream/Tap ingestion, Skyreader RSS polling, proactive backfill, TTL cleanup

## Modules

| Type | Files |
|------|-------|
| Indexing | `ThinAppViewIndexer`, `RenderFieldExtractor` |
| Storage | `SQLiteThinAppViewStore`, `PostgresThinAppViewStore`, `ThinAppViewStore` |
| Projection cache | `AppViewProjectionCacheStore`, `RedisAppViewProjectionCacheStore`, SQLite/Postgres rollback implementations, `RedisProjectionCacheRuntime` |
| Worker | `ThinAppViewWorkerRuntime`, `FirehoseSubscriber`, Tap consumers, `ThinAppViewRssFeedPollJob`, `ThinAppViewTtlCleanupJob`, `ThinAppViewProactiveBackfillJob` |
| Config | `ThinAppViewConfig`, `RuntimeEnvironment`, `PostgresConfig` |
| Query | `ThinAppViewQuerySupport`, `ThinAppViewModels` |

## Tests

```bash
cd packages/swift/ThinAppViewCore
swift test
```

CI runs this explicitly in the **`charybdis`** job.

## Architecture

See [docs/architecture/appview.md](../../../docs/architecture/appview.md), [docs/architecture/redis.md](../../../docs/architecture/redis.md), and [docs/wiki/Thin-AppView.md](../../../docs/wiki/Thin-AppView.md).

## Related

- [Charybdis test plan](../../../docs/test-plans/worker.md)
- [AppView test plan](../../../docs/test-plans/appview.md)
