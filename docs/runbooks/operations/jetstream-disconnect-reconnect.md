# Jetstream Disconnect and Reconnect

1. Confirm `connection_state`, disconnect time, and bounded reason in Operations → Ingestion.
2. Compare the last received and committed `time_us` cursors. Never resume from `commit.rev`.
3. Allow the worker to reconnect from the committed cursor minus five seconds.
4. Verify committed cursor advancement and a stable receive-to-commit backlog for five minutes.
5. If received is ahead of committed or the pump overflowed, confirm and scope the generated gap.

Do not manually advance the committed cursor. Duplicate overlap is safe; skipping is not.
