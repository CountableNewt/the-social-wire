> Historical reference: this document describes infrastructure or a cutover stage retired in #963. Do not apply its deployment commands. Current runtime, initialization and recovery guidance is in `docs/architecture/standalone-reader.md`.

# Embedded scheduling cutover

The Social Wire Web backend prepares Standard.site records locally. Immediate article publishing continues to write through the viewer's PDS session. Scheduled posts and articles keep their `at.skej.schedule` records, schedule URIs, and publication keys. Standalone AppView owns the queue and runs the scheduler after cutover.

## Development cutover

1. Before deploying this cleanup to Development, stop the standalone Skej scheduler for that environment and confirm it has stopped. Preserve its volume and record any in-flight jobs. Pause legacy client writes so no new jobs enter the old queue. The embedded scheduler now runs unconditionally and imports legacy PDS schedules once per account.
2. Rename the backend variables to `SCHEDULING_PUBLIC_ORIGIN=https://api.testing.thesocialwire.app` and `SCHEDULING_STORAGE_KEY`. Preserve the exact existing 32-byte Base64 storage key so existing encrypted scheduling sessions remain readable. Remove the retired `EMBEDDED_SCHEDULING_*` variables after transferring these values. Missing origin or key prevents startup.
3. Deploy the code to Development. Remove `NEXT_PUBLIC_USE_EMBEDDED_SCHEDULING`; Web always uses the Social Wire edge. Keep `NEXT_PUBLIC_ENABLE_POST_SCHEDULING=true` where Content Calendar is live. Verify `/oauth/scheduling/client-metadata.json`, then reconnect personal and linked brand accounts as needed and verify scheduled posts and articles.
4. Reopening Content Calendar triggers idempotent queue repair/import. AppView preserves schedule identities and holds jobs with recorded prior attempts for review. Queued jobs already past their schedule can run after import.
5. Keep the storage key with protected recovery material. The separate `scheduling.sqlite` database lives alongside the AppView database on its persistent volume. Run one scheduler deployment against that volume. Include the database in backups by setting `SQLITE_DB_PATH=/data/scheduling.sqlite` when invoking `sqlite-backup` with a separate destination.

Skej OAuth grants are tied to the old client ID. They cannot be moved to the new client by copying the SQLite database. Keep the old volume for queue reconciliation and recovery. For environments with server-side Pro gating, the selected scheduling DID must be entitled, including a linked brand DID.

AnyPub's private drafts, local assets and publish history live on its own volume. Export or preserve that volume and account for pending publications before deleting it. Published PDS records remain available independently of that volume.

If the new scheduler has accepted connections or schedules, pause scheduling and reconcile the PDS records against both queue snapshots before re-enabling any old worker. Keep a single publisher active throughout. Repeat the cutover for Production only after Development acceptance and before retiring the Production services.
