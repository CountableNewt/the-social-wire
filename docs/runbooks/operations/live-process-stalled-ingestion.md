# Live Process With Stalled Ingestion

1. Verify `/livez` is healthy and `/freshness` is degraded.
2. Inspect inflight messages, queue depth, last received age, and last committed age.
3. Open recent `worker.jetstream.message`, `worker.index.commit`, and database spans.
4. If records fail, classify the bounded error type and inspect `appview_recovery_failures` by hash.
5. Restart only after preserving the durable committed cursor. Confirm the five-second replay overlap.
