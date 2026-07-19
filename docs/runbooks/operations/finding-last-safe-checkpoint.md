# Finding the Last Safe Checkpoint

The safe checkpoint is `appview_ingestion_stream_state.last_committed_cursor`. It advances only after the idempotent content mutation and derived cache/counter work succeed.

1. Record the committed cursor, event timestamp, and observed timestamp.
2. Compare it with the latest content index write and recovery failures.
3. Start replay five seconds before that cursor.
4. When the upgraded stream has only a seeded received cursor, use the automatic thirty-second first-start rewind.

The legacy per-repository checkpoint table is rollback evidence only.
