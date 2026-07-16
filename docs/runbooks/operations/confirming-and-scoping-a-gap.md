# Confirming and Scoping a Gap

1. Validate the detection reason: regression, disconnect window, idle connection, stale commit, backlog, overflow, or failed record.
2. Bound start/end `time_us` cursors and affected collection allowlist.
3. Run a dry-run estimate in Development.
4. Mark the gap Confirmed only when evidence shows completeness risk.
5. Use Jetstream Replay within retention. Use PDS Reconciliation outside it and note that historical deletes cannot be reconstructed with equal certainty.
