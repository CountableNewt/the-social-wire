# Running, Pausing, Resuming, and Validating Backfills

1. Run the dry-run and review count, duration, conflicts, filters, bounds, and delete warning.
2. Add a specific operator audit note. Production additionally requires typing `PRODUCTION`.
3. Queue the job and confirm its database lease, heartbeat, and first checkpoint.
4. Pause or cancel between batches. Resume from the persisted checkpoint with the five-second overlap.
5. Validate indexed counts, failures, unread/cache derivations, and response freshness before resolving the gap.
