# Disabling or Rolling Back Telemetry

1. Disable webhook delivery with `OPERATIONS_ALERT_DELIVERY_ENABLED=false`.
2. Disable recovery mutations with `OPERATIONS_RECOVERY_ENABLED=false`.
3. Disable sampled telemetry with `OPERATIONS_TELEMETRY_ENABLED=false`.
4. Redeploy the previous service binaries if needed. Do not drop operations tables.
5. Keep the new committed cursor state intact unless executing the documented binary rollback; legacy checkpoints remain available for that rollback.
6. Verify reader, Gateway, and AppView contracts remain unchanged.
