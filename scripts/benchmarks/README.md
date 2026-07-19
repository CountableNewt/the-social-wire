# Operations Telemetry Overhead Benchmark

Run equivalent Gateway or AppView deployments with telemetry disabled and enabled, then compare the same authenticated route:

```sh
BASELINE_ORIGIN=https://baseline.example \
TELEMETRY_ORIGIN=https://telemetry.example \
BENCHMARK_PATH=/v1/appview/bootstrap-stream \
BENCHMARK_AUTHORIZATION='Bearer …' \
BENCHMARK_DPOP='…' \
bun scripts/benchmarks/operations-overhead.ts
```

Use the same database snapshot, worker cursor, region, machine size, request count, and concurrency for both runs. Repeat for bootstrap, entries, unread counts, and sidebar. For ingestion, run identical bounded Jetstream replay jobs against isolated database snapshots and compare the emitted `socialwire.ingestion.events_total` rate and commit-lag p95.

The command fails if throughput or p95 regresses by more than 5%. `OperationsTelemetryBufferTests` separately verifies that exporter failure cannot grow the in-process queue beyond its configured bound.
